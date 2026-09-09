package handler_test

import (
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// createStaleOverrideTraining makes a training out of the items given and
// returns its id with the id of every item, in order.
func createStaleOverrideTraining(t *testing.T, app *fiber.App, coachToken string, items []map[string]interface{}) (string, []string) {
	t.Helper()
	status, training := postJSON(t, app, "/api/trainings", coachToken, map[string]interface{}{
		"title":         "Stale override training",
		"training_type": "climbing",
		"items":         items,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}
	raw := training["items"].([]interface{})
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		ids = append(ids, item.(map[string]interface{})["id"].(string))
	}
	return training["id"].(string), ids
}

// editStaleOverrideTraining rewrites the items of a training, which keeps the id
// of every item sent back with one, and asserts the coach is not blocked by the
// week that already prescribes those items.
func editStaleOverrideTraining(t *testing.T, app *fiber.App, coachToken, trainingID string, items []map[string]interface{}) {
	t.Helper()
	status, result := putJSON(t, app, "/api/trainings/"+trainingID, coachToken, map[string]interface{}{
		"title":         "Stale override training",
		"training_type": "climbing",
		"items":         items,
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 editing the training under a prescribed week, got %d: %v", status, result)
	}
}

// The scenario Krakoer/crimpy#84 describes: an override validated against the
// item as it stood when the week was saved, and a training edited afterwards
// without the week being consulted. The two are each valid alone and refused
// together, so the snapshot has to check the merge again and drop what no longer
// holds rather than freeze a shape no client can run.
func TestSessionHandler_CreateSession_DropsOverrideStaleAfterTrainingEdit(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "stalemax")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)
	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
		{"type": "exercise", "reps": 6},
	})
	amrapID, steadyID := itemIDs[0], itemIDs[1]

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": amrapID, "overrides": map[string]interface{}{"reps_is_max": true}},
			{"item_id": steadyID, "overrides": map[string]interface{}{"reps": 12}},
		},
	})

	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{
			"id": amrapID, "type": "exercise", "reps": 8,
			"variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{
					"assessment_id": assessmentID, "percent": 50, "fallback": 10,
				},
			},
		},
		{"id": steadyID, "type": "exercise", "reps": 6},
	})

	session := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})

	items := prescriptionItems(t, sessionPrescription(t, session))
	if len(items) != 2 {
		t.Fatalf("Expected 2 items in the snapshot, got %d", len(items))
	}

	amrap := items[0].(map[string]interface{})
	if amrap["reps_is_max"] != false {
		t.Errorf("Expected the stale AMRAP marker dropped, got %v", amrap["reps_is_max"])
	}
	targets, ok := amrap["variable_targets"].(map[string]interface{})
	if !ok || targets["reps"] == nil {
		t.Fatalf("Expected the training's own reps target in the snapshot, got %v", amrap["variable_targets"])
	}
	if amrap["reps"] != float64(8) {
		t.Errorf("Expected the item to fall back on the training's rep count 8, got %v", amrap["reps"])
	}

	steady := items[1].(map[string]interface{})
	if steady["reps"] != float64(12) {
		t.Errorf("Expected the still valid override on the other item untouched, got %v", steady["reps"])
	}
}

// The mirror direction: the override sets the percentage and the edit turns the
// rep count open. The merged item is the same refused shape, so the same
// override goes.
func TestSessionHandler_CreateSession_DropsOverrideStaleAfterMarkerAddedToItem(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "stalemirror")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)
	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
	})
	itemID := itemIDs[0]

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{
				"variable_targets": map[string]interface{}{
					"reps": map[string]interface{}{
						"assessment_id": assessmentID, "percent": 50, "fallback": 10,
					},
				},
			}},
		},
	})

	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{"id": itemID, "type": "exercise", "reps_is_max": true},
	})

	session := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})

	item := prescriptionItems(t, sessionPrescription(t, session))[0].(map[string]interface{})
	if item["reps_is_max"] != true {
		t.Errorf("Expected the training's open rep count in the snapshot, got %v", item["reps_is_max"])
	}
	if item["variable_targets"] != nil {
		t.Errorf("Expected the stale reps target dropped, got %v", item["variable_targets"])
	}
}

// The regression that matters most: revalidating must not cost the coach the
// overrides that still hold. A training edited around an override the merged
// item still takes leaves that override applied.
func TestSessionHandler_CreateSession_KeepsValidOverrideAcrossTrainingEdit(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "stalekeep")

	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
	})
	itemID := itemIDs[0]

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{"reps": 12, "rest_seconds": 90}},
		},
	})

	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{"id": itemID, "type": "exercise", "reps": 5, "duration": 30},
	})

	session := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})

	item := prescriptionItems(t, sessionPrescription(t, session))[0].(map[string]interface{})
	if item["reps"] != float64(12) {
		t.Errorf("Expected the override rep count 12 still merged, got %v", item["reps"])
	}
	if item["rest_seconds"] != float64(90) {
		t.Errorf("Expected the override rest 90 still merged, got %v", item["rest_seconds"])
	}
	if item["duration"] != float64(30) {
		t.Errorf("Expected the edited duration 30 under the override, got %v", item["duration"])
	}
}

// An override on a block the edit retyped goes the same way: a rest the emom
// cannot carry is what both write paths refuse, so the athlete plays the emom
// the training now prescribes.
func TestSessionHandler_CreateSession_DropsOverrideStaleAfterBlockRetyped(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "staleretype")

	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "circuit", "cycles": 3, "rest_seconds": 60, "items": []map[string]interface{}{
			{"type": "exercise", "reps": 5},
		}},
	})
	blockID := itemIDs[0]

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": blockID, "overrides": map[string]interface{}{"rest_seconds": 45}},
		},
	})

	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{"id": blockID, "type": "emom", "cycles": 3, "interval_seconds": 60, "items": []map[string]interface{}{
			{"type": "exercise", "reps": 5},
		}},
	})

	session := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})

	item := prescriptionItems(t, sessionPrescription(t, session))[0].(map[string]interface{})
	if item["type"] != "emom" {
		t.Fatalf("Expected the retyped emom in the snapshot, got %v", item["type"])
	}
	if item["rest_seconds"] != nil {
		t.Errorf("Expected the stale rest dropped from the emom, got %v", item["rest_seconds"])
	}
	if item["interval_seconds"] != float64(60) {
		t.Errorf("Expected the emom interval 60, got %v", item["interval_seconds"])
	}
}

// weekOverrideEntries reads the override entries of the single session of a week
// response, keyed by the item they target, so a test can read what the entry
// says about the override and not only the override itself.
func weekOverrideEntries(t *testing.T, app *fiber.App, url, token string) map[string]map[string]interface{} {
	t.Helper()
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, url, nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 reading %s, got %d", url, resp.StatusCode)
	}
	var week map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&week); err != nil {
		t.Fatalf("Failed to decode the week: %v", err)
	}
	return weekOverrideEntriesOf(t, week)
}

// weekOverrideEntriesOf reads the same entries out of a week body already in
// hand, so an upsert echo can be read without a second request.
func weekOverrideEntriesOf(t *testing.T, week map[string]interface{}) map[string]map[string]interface{} {
	t.Helper()
	sessions := week["sessions"].([]interface{})
	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session in the week, got %d", len(sessions))
	}
	byItem := map[string]map[string]interface{}{}
	for _, raw := range sessions[0].(map[string]interface{})["overrides"].([]interface{}) {
		o := raw.(map[string]interface{})
		byItem[o["item_id"].(string)] = o
	}
	return byItem
}

// weekOverrides reads the overrides of the single session of a week response,
// keyed by the item they target.
func weekOverrides(t *testing.T, app *fiber.App, url, token string) map[string]map[string]interface{} {
	t.Helper()
	byItem := map[string]map[string]interface{}{}
	for itemID, entry := range weekOverrideEntries(t, app, url, token) {
		byItem[itemID] = entry["overrides"].(map[string]interface{})
	}
	return byItem
}

// The athlete's own preview merges the overrides client side, so one the
// training no longer takes would show a prescription the session they then start
// refuses to freeze. The read has to hide exactly what the snapshot drops, and
// nothing else. The coach read is left whole on purpose: the editor sends its
// week back as it read it, and the frozen session check compares the two.
func TestWeekHandler_GetMyWeek_HidesOverrideStaleAfterTrainingEdit(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "stalemyweek")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)
	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
		{"type": "exercise", "reps": 6},
	})
	amrapID, steadyID := itemIDs[0], itemIDs[1]

	prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": amrapID, "overrides": map[string]interface{}{"reps_is_max": true}},
			{"item_id": steadyID, "overrides": map[string]interface{}{"reps": 12}},
		},
	})

	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{
			"id": amrapID, "type": "exercise", "reps": 8,
			"variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{
					"assessment_id": assessmentID, "percent": 50, "fallback": 10,
				},
			},
		},
		{"id": steadyID, "type": "exercise", "reps": 6},
	})

	mine := weekOverrides(t, app, fmt.Sprintf("/api/user/programs/%s/weeks/1", programID), userToken)
	if _, present := mine[amrapID]; present {
		t.Errorf("Expected the stale override hidden from the athlete week, got %v", mine[amrapID])
	}
	steady, kept := mine[steadyID]
	if !kept {
		t.Fatalf("Expected the still valid override kept on the athlete week, got %v", mine)
	}
	if steady["reps"] != float64(12) {
		t.Errorf("Expected the valid override rep count 12, got %v", steady["reps"])
	}

	coach := weekOverrides(t, app, weekURL(userID, programID, 1), coachToken)
	if len(coach) != 2 {
		t.Errorf("Expected the coach week to keep both overrides, got %v", coach)
	}
}

// staleOverrideEntry reads one override entry of the coach week and asserts the
// override itself came back, since the flag is only ever added beside it.
func staleOverrideEntry(t *testing.T, entries map[string]map[string]interface{}, itemID string) map[string]interface{} {
	t.Helper()
	entry, present := entries[itemID]
	if !present {
		t.Fatalf("Expected the override on item %s in the coach week, got %v", itemID, entries)
	}
	if _, carried := entry["overrides"].(map[string]interface{}); !carried {
		t.Fatalf("Expected the override itself beside the flag, got %v", entry)
	}
	return entry
}

// The issue Krakoer/crimpy#89 describes: the coach prescribes an AMRAP, edits
// the training months later in a way the merged item refuses, and reads a week
// that still promises something the athlete will never play. The stored row must
// come back untouched, because the editor sends the week back as it read it, so
// the only thing the coach can be told with is a flag computed beside it.
func TestWeekHandler_GetWeek_FlagsStaleOverrideWithoutHidingIt(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "staleflag")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)
	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
		{"type": "exercise", "reps": 6},
	})
	amrapID, steadyID := itemIDs[0], itemIDs[1]

	prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": amrapID, "overrides": map[string]interface{}{"reps_is_max": true}},
			{"item_id": steadyID, "overrides": map[string]interface{}{"reps": 12}},
		},
	})

	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{
			"id": amrapID, "type": "exercise", "reps": 8,
			"variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{
					"assessment_id": assessmentID, "percent": 50, "fallback": 10,
				},
			},
		},
		{"id": steadyID, "type": "exercise", "reps": 6},
	})

	entries := weekOverrideEntries(t, app, weekURL(userID, programID, 1), coachToken)
	if len(entries) != 2 {
		t.Fatalf("Expected both overrides returned to the coach, got %v", entries)
	}

	amrap := staleOverrideEntry(t, entries, amrapID)
	if amrap["override_stale"] != true {
		t.Errorf("Expected the stale AMRAP override flagged, got %v", amrap)
	}
	if amrap["overrides"].(map[string]interface{})["reps_is_max"] != true {
		t.Errorf("Expected the stored override returned verbatim beside the flag, got %v", amrap["overrides"])
	}
	// Krakoer/crimpy#100 replaced the prose reason with the same refusal
	// attributed to the fields it is about. Here the open rep count and the
	// percentage the item now reads it off cannot both stand, and either one
	// going answers it, so both are named.
	if fields := staleFieldNames(t, amrap); !slices.Equal(fields, []string{"reps_is_max", "variable_targets"}) {
		t.Errorf("Expected the refusal attributed to the two fields it is about, got %v", fields)
	}

	steady := staleOverrideEntry(t, entries, steadyID)
	if steady["override_stale"] != false {
		t.Errorf("Expected the still valid override unflagged, got %v", steady)
	}
	if _, given := steady["stale_fields"]; given {
		t.Errorf("Expected no attribution on a valid override, got %v", steady["stale_fields"])
	}

	// The athlete is the one the flag is about: it says this override is not
	// being applied, so the two reads have to agree on which one that is.
	mine := weekOverrides(t, app, fmt.Sprintf("/api/user/programs/%s/weeks/1", programID), userToken)
	if _, present := mine[amrapID]; present {
		t.Errorf("Expected the flagged override left out of the athlete week, got %v", mine[amrapID])
	}
	if _, kept := mine[steadyID]; !kept {
		t.Errorf("Expected the unflagged override applied for the athlete, got %v", mine)
	}
}

// The regression that matters most: flagging must not cost the coach the
// overrides that still hold. A training edited around an override the merged
// item still takes leaves that override unflagged.
func TestWeekHandler_GetWeek_DoesNotFlagValidOverride(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, userID, programID := setupAssessmentProgramApp(t, "staleok")

	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
	})
	itemID := itemIDs[0]

	prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{"reps": 12, "rest_seconds": 90}},
		},
	})

	fresh := staleOverrideEntry(t, weekOverrideEntries(t, app, weekURL(userID, programID, 1), coachToken), itemID)
	if fresh["override_stale"] != false {
		t.Errorf("Expected an override just written unflagged, got %v", fresh)
	}

	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{"id": itemID, "type": "exercise", "reps": 5, "duration": 30},
	})

	edited := staleOverrideEntry(t, weekOverrideEntries(t, app, weekURL(userID, programID, 1), coachToken), itemID)
	if edited["override_stale"] != false {
		t.Errorf("Expected the override the edited item still takes unflagged, got %v", edited)
	}
	if _, given := edited["stale_fields"]; given {
		t.Errorf("Expected no attribution on a valid override, got %v", edited["stale_fields"])
	}
	if edited["overrides"].(map[string]interface{})["reps"] != float64(12) {
		t.Errorf("Expected the override returned unchanged, got %v", edited["overrides"])
	}
}

// weekUpsertPayloadFromRead turns a coach week read into the payload the editor
// sends back, keeping every override entry exactly as it was read.
func weekUpsertPayloadFromRead(week map[string]interface{}) map[string]interface{} {
	sessions := make([]map[string]interface{}, 0)
	for _, raw := range week["sessions"].([]interface{}) {
		s := raw.(map[string]interface{})
		session := map[string]interface{}{
			"id":          s["id"],
			"training_id": s["training_id"],
			"overrides":   s["overrides"],
		}
		for _, mode := range []string{"day_of_week", "times_per_week"} {
			if value, set := s[mode]; set {
				session[mode] = value
			}
		}
		if s["is_everyday"] == true {
			session["is_everyday"] = true
		}
		if notes, set := s["notes"]; set {
			session["notes"] = notes
		}
		sessions = append(sessions, session)
	}
	payload := map[string]interface{}{"sessions": sessions}
	if notes, set := week["notes"]; set {
		payload["notes"] = notes
	}
	return payload
}

// The constraint the flag must not break: checkFrozenSession refuses a locked
// session whose overrides come back changed, so the coach read has to stay
// something the editor can send back as it read it. The flag rides beside the
// override rather than inside it, and the write path ignores it. The override
// round tripped here is a valid one, unflagged: what is pinned is that the two
// new fields riding beside it do not count as a change.
func TestWeekHandler_UpsertWeek_RoundTripsCoachWeekWithFlagFields(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "staleround")

	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
	})
	itemID := itemIDs[0]

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{"reps": 12}},
		},
	})
	playProgramSession(t, app, userToken, trainingID, programSessionID)

	week := getWeek(t, app, coachToken, userID, programID, 1)
	if !weekSessionLocks(week)[0] {
		t.Fatalf("Expected the played session locked, got %v", week["sessions"])
	}

	echoed := upsertWeekSessions(t, app, coachToken, userID, programID, 1, weekUpsertPayloadFromRead(week))

	if !weekSessionLocks(echoed)[0] {
		t.Errorf("Expected the session to stay locked through the round trip, got %v", echoed["sessions"])
	}
	// The echo answers the flag against the training as it stands, as GET does,
	// so a valid override round tripped through a save comes back unflagged.
	// Pinned so the field cannot quietly stop being emitted there.
	for _, echoedEntry := range weekOverrideEntriesOf(t, echoed) {
		if echoedEntry["override_stale"] != false {
			t.Errorf("Expected the upsert echo to answer the flag as false, got %v", echoedEntry)
		}
	}
	entry := staleOverrideEntry(t, weekOverrideEntries(t, app, weekURL(userID, programID, 1), coachToken), itemID)
	if entry["override_stale"] != false {
		t.Errorf("Expected the round tripped override unflagged, got %v", entry)
	}
	if entry["overrides"].(map[string]interface{})["reps"] != float64(12) {
		t.Errorf("Expected the frozen override intact after the round trip, got %v", entry["overrides"])
	}
}

// What saving a flagged week does today, pinned so it is documented rather than
// folklore. The write path refuses the very override the read flags, with the
// same reason the flag carries, so a week read with a stale override cannot be
// sent back unchanged. This pins the unlocked session, which is the cheaper half
// to set up and the one where the 400 is the intended remediation: the coach
// answers it by dropping or rewriting the override, as the tail of this test
// does. Krakoer/crimpy#96 is the locked variant of the same refusal, where
// checkFrozenSession also forbids changing the override and the coach has no
// answer left.
func TestWeekHandler_UpsertWeek_RefusesWeekCarryingStaleOverride(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, userID, programID := setupAssessmentProgramApp(t, "stalesave")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)
	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
	})
	itemID := itemIDs[0]

	prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{"reps_is_max": true}},
		},
	})

	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{
			"id": itemID, "type": "exercise", "reps": 8,
			"variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{
					"assessment_id": assessmentID, "percent": 50, "fallback": 10,
				},
			},
		},
	})

	week := getWeek(t, app, coachToken, userID, programID, 1)
	if weekSessionLocks(week)[0] {
		t.Fatalf("Expected the session unlocked, got %v", week["sessions"])
	}
	flagged := staleOverrideEntry(t, weekOverrideEntries(t, app, weekURL(userID, programID, 1), coachToken), itemID)
	if flagged["override_stale"] != true {
		t.Fatalf("Expected the override flagged stale before the save, got %v", flagged)
	}

	status, message := upsertWeekError(t, app, coachToken, userID, programID, 1, weekUpsertPayloadFromRead(week))
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 saving the week the coach just read, got %d: %s", status, message)
	}
	if !strings.Contains(message, itemID) {
		t.Errorf("Expected the refusal to name the stale override item, got %q", message)
	}
	// The wording is still the one the read hands over, now attributed to the
	// field it is about rather than handed over loose: what the coach is told
	// when a save is refused and what the week says about the same override
	// have to stay one wording, or the two cannot be recognised as one refusal.
	assertRefusedWithOneAttributedReason(t, message, staleFieldReasons(t, flagged))

	sessionID := week["sessions"].([]interface{})[0].(map[string]interface{})["id"]
	remediated := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{
			"id":          sessionID,
			"training_id": trainingID,
			"day_of_week": 0,
			"overrides":   []map[string]interface{}{},
		}},
	})
	kept := remediated["sessions"].([]interface{})[0].(map[string]interface{})["overrides"].([]interface{})
	if len(kept) != 0 {
		t.Errorf("Expected dropping the stale override to let the week save, got %v", kept)
	}
}

// lockedStaleOverrideWeek stages the state Krakoer/crimpy#96 is about: a week
// whose only session carries one override the training item no longer takes and
// one it still does, played by the athlete after the training edit, so the
// session is locked and its prescription was frozen without the stale override.
func lockedStaleOverrideWeek(t *testing.T, app *fiber.App, coachToken, userToken, userID, programID string) (trainingID, staleItemID, validItemID string) {
	t.Helper()

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)
	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
		{"type": "exercise", "reps": 6},
	})
	staleItemID, validItemID = itemIDs[0], itemIDs[1]

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": staleItemID, "overrides": map[string]interface{}{"reps_is_max": true}},
			{"item_id": validItemID, "overrides": map[string]interface{}{"reps": 12}},
		},
	})

	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{
			"id": staleItemID, "type": "exercise", "reps": 8,
			"variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{
					"assessment_id": assessmentID, "percent": 50, "fallback": 10,
				},
			},
		},
		{"id": validItemID, "type": "exercise", "reps": 6},
	})

	played := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})
	snapshot := prescriptionItems(t, sessionPrescription(t, played))
	if len(snapshot) != 2 {
		t.Fatalf("Expected 2 items in the frozen prescription, got %d", len(snapshot))
	}
	if snapshot[0].(map[string]interface{})["reps_is_max"] != false {
		t.Fatalf("Expected the prescription frozen without the stale override, got %v", snapshot[0])
	}
	if snapshot[1].(map[string]interface{})["reps"] != float64(12) {
		t.Fatalf("Expected the still valid override frozen into the prescription, got %v", snapshot[1])
	}
	return trainingID, staleItemID, validItemID
}

// The deadlock Krakoer/crimpy#96 describes, end to end. checkFrozenSession
// refuses the save unless a locked session's overrides come back exactly as
// stored, and the write path used to refuse that very echo for being stale, so
// every save of the week failed and the coach had no move left. The stale row is
// inert on a locked session, since the frozen prescription was taken without it
// and the athlete week hides it, so the save goes through and the rest of the
// week stays editable.
func TestWeekHandler_UpsertWeek_LetsLockedSessionKeepItsStaleOverride(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "stalelocked")

	trainingID, staleItemID, validItemID := lockedStaleOverrideWeek(t, app, coachToken, userToken, userID, programID)

	week := getWeek(t, app, coachToken, userID, programID, 1)
	if !weekSessionLocks(week)[0] {
		t.Fatalf("Expected the played session locked, got %v", week["sessions"])
	}
	flagged := staleOverrideEntry(t, weekOverrideEntriesOf(t, week), staleItemID)
	if flagged["override_stale"] != true {
		t.Fatalf("Expected the override flagged stale before the save, got %v", flagged)
	}

	echoed := upsertWeekSessions(t, app, coachToken, userID, programID, 1, weekUpsertPayloadFromRead(week))

	if !weekSessionLocks(echoed)[0] {
		t.Errorf("Expected the session to stay locked through the save, got %v", echoed["sessions"])
	}
	// The echo answers the flag as a read of the same rows does, so saving the
	// week cannot tell the coach the override started applying.
	echoedEntry := staleOverrideEntry(t, weekOverrideEntriesOf(t, echoed), staleItemID)
	if echoedEntry["override_stale"] != true {
		t.Errorf("Expected the echo to keep flagging the override it let through, got %v", echoedEntry)
	}

	// Krakoer/crimpy#84 never touches the stored row, and letting the save
	// through must not either.
	entries := weekOverrideEntries(t, app, weekURL(userID, programID, 1), coachToken)
	if len(entries) != 2 {
		t.Fatalf("Expected both overrides still stored after the save, got %v", entries)
	}
	stored := staleOverrideEntry(t, entries, staleItemID)
	if stored["override_stale"] != true {
		t.Errorf("Expected the kept override still flagged after the save, got %v", stored)
	}
	if stored["overrides"].(map[string]interface{})["reps_is_max"] != true {
		t.Errorf("Expected the stored override intact after the save, got %v", stored["overrides"])
	}
	valid := staleOverrideEntry(t, entries, validItemID)
	if valid["override_stale"] != false {
		t.Errorf("Expected the still valid override unflagged, got %v", valid)
	}
	if valid["overrides"].(map[string]interface{})["reps"] != float64(12) {
		t.Errorf("Expected the still valid override intact after the save, got %v", valid["overrides"])
	}

	// The athlete is still not offered what the save let through.
	mine := weekOverrides(t, app, fmt.Sprintf("/api/user/programs/%s/weeks/1", programID), userToken)
	if _, present := mine[staleItemID]; present {
		t.Errorf("Expected the kept override still left out of the athlete week, got %v", mine[staleItemID])
	}
	if _, kept := mine[validItemID]; !kept {
		t.Errorf("Expected the still valid override applied for the athlete, got %v", mine)
	}

	// The point of the fix: the week is editable again around the frozen
	// session, which is what the deadlock cost.
	rescheduled := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{
			"id":          week["sessions"].([]interface{})[0].(map[string]interface{})["id"],
			"training_id": trainingID,
			"day_of_week": 3,
			"notes":       "moved to Thursday",
			"overrides":   week["sessions"].([]interface{})[0].(map[string]interface{})["overrides"],
		}},
	})
	if rescheduled["sessions"].([]interface{})[0].(map[string]interface{})["day_of_week"] != float64(3) {
		t.Errorf("Expected the frozen session rescheduled, got %v", rescheduled["sessions"])
	}
}

// The guard that must not move: a locked session gets a pass on validity, never
// a pass on change. The coach cannot answer the staleness by rewriting the
// override or by dropping it, because that would rewrite the prescription the
// athlete already played behind them.
func TestWeekHandler_UpsertWeek_StillRefusesChangedStaleOverrideOnLockedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "stalefrozen")

	trainingID, staleItemID, validItemID := lockedStaleOverrideWeek(t, app, coachToken, userToken, userID, programID)

	week := getWeek(t, app, coachToken, userID, programID, 1)
	if !weekSessionLocks(week)[0] {
		t.Fatalf("Expected the played session locked, got %v", week["sessions"])
	}
	sessionID := week["sessions"].([]interface{})[0].(map[string]interface{})["id"]
	valid := map[string]interface{}{"item_id": validItemID, "overrides": map[string]interface{}{"reps": 12}}

	for _, attempt := range []struct {
		change    string
		overrides []map[string]interface{}
	}{
		{"rewritten", []map[string]interface{}{
			{"item_id": staleItemID, "overrides": map[string]interface{}{"reps": 5}},
			valid,
		}},
		{"dropped", []map[string]interface{}{valid}},
	} {
		status, message := upsertWeekError(t, app, coachToken, userID, programID, 1, map[string]interface{}{
			"sessions": []map[string]interface{}{{
				"id":          sessionID,
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides":   attempt.overrides,
			}},
		})
		if status != fiber.StatusBadRequest {
			t.Fatalf("Expected 400 with the stale override %s on a played session, got %d: %s", attempt.change, status, message)
		}
		if !strings.Contains(message, "overrides cannot be changed") {
			t.Errorf("Expected the frozen session refusal with the override %s, got %q", attempt.change, message)
		}
	}

	stored := staleOverrideEntry(t, weekOverrideEntries(t, app, weekURL(userID, programID, 1), coachToken), staleItemID)
	if stored["overrides"].(map[string]interface{})["reps_is_max"] != true {
		t.Errorf("Expected the stored override intact after the refusals, got %v", stored["overrides"])
	}
}
