package handler_test

import (
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
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

// weekOverrides reads the overrides of the single session of a week response,
// keyed by the item they target.
func weekOverrides(t *testing.T, app *fiber.App, url, token string) map[string]map[string]interface{} {
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
	sessions := week["sessions"].([]interface{})
	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session in the week, got %d", len(sessions))
	}
	byItem := map[string]map[string]interface{}{}
	for _, raw := range sessions[0].(map[string]interface{})["overrides"].([]interface{}) {
		o := raw.(map[string]interface{})
		byItem[o["item_id"].(string)] = o["overrides"].(map[string]interface{})
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
