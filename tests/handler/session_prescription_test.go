package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func playSession(t *testing.T, app *fiber.App, userToken string, links map[string]interface{}) map[string]interface{} {
	t.Helper()
	payload := map[string]interface{}{
		"name":     "Prescribed hangboard",
		"notes":    "",
		"activity": 0,
		"origin":   "played",
		"duration": 600,
	}
	for k, v := range links {
		payload[k] = v
	}
	body, _ := json.Marshal(payload)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, userToken))
	if err != nil {
		t.Fatalf("Failed to play session: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 playing session, got %d", resp.StatusCode)
	}
	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	return created
}

func getSessionJSON(t *testing.T, app *fiber.App, userToken, sessionID string) map[string]interface{} {
	t.Helper()
	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, "/api/sessions/"+sessionID, nil, userToken))
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 getting session, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	session, ok := result["session"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected a session in the response, got %v", result)
	}
	return session
}

func sessionPrescription(t *testing.T, session map[string]interface{}) map[string]interface{} {
	t.Helper()
	prescription, ok := session["prescription"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected a prescription on the session, got %v", session["prescription"])
	}
	return prescription
}

func prescriptionItems(t *testing.T, prescription map[string]interface{}) []interface{} {
	t.Helper()
	items, ok := prescription["items"].([]interface{})
	if !ok {
		t.Fatalf("Expected items on the prescription, got %v", prescription["items"])
	}
	return items
}

func replaceTrainingItems(t *testing.T, app *fiber.App, coachToken, trainingID string, items []map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"title":         "Hangboard Training",
		"training_type": "hangboard",
		"items":         items,
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/trainings/"+trainingID, body, coachToken))
	if err != nil {
		t.Fatalf("Failed to update training: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 updating training, got %d", resp.StatusCode)
	}
}

func TestSessionHandler_CreateSession_SnapshotsResolvedPrescription(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "presnap")
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"notes":       "Warm up first",
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
				},
			},
		},
	})
	programSessionID := weekSessionIDs(created)[0]

	session := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})

	prescription := sessionPrescription(t, session)
	if prescription["id"] != trainingID {
		t.Errorf("Expected the training id %s, got %v", trainingID, prescription["id"])
	}
	if prescription["title"] != "Hangboard Training" {
		t.Errorf("Expected the training title, got %v", prescription["title"])
	}
	if prescription["program_session_id"] != programSessionID {
		t.Errorf("Expected program_session_id %s, got %v", programSessionID, prescription["program_session_id"])
	}
	if prescription["coach_notes"] != "Warm up first" {
		t.Errorf("Expected the coach notes, got %v", prescription["coach_notes"])
	}

	items := prescriptionItems(t, prescription)
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	item := items[0].(map[string]interface{})
	if item["reps"] != float64(5) {
		t.Errorf("Expected the override reps 5 merged into the snapshot, got %v", item["reps"])
	}
}

// The point of the snapshot: the training and the overrides stay editable, and
// the played session keeps describing what the athlete was actually asked to do.
func TestSessionHandler_CreateSession_PrescriptionSurvivesTrainingEdit(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "presurv")
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
				},
			},
		},
	})
	programSessionID := weekSessionIDs(created)[0]

	played := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})
	sessionID := played["id"].(string)

	replaceTrainingItems(t, app, coachToken, trainingID, []map[string]interface{}{
		{
			"type":             "repeater",
			"reps":             12,
			"worktime_seconds": 10,
			"rest_seconds":     30,
			"hand":             "both",
			"granularity":      "uniform",
			"loads":            []map[string]interface{}{{"value": 40, "unit": "kg"}},
		},
	})

	prescription := sessionPrescription(t, getSessionJSON(t, app, userToken, sessionID))
	items := prescriptionItems(t, prescription)
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	item := items[0].(map[string]interface{})
	if item["reps"] != float64(5) {
		t.Errorf("Expected the snapshot to keep reps 5 after the training was rewritten, got %v", item["reps"])
	}
	if item["rest_seconds"] != float64(60) {
		t.Errorf("Expected the snapshot to keep rest_seconds 60, got %v", item["rest_seconds"])
	}
	loads, ok := item["loads"].([]interface{})
	if !ok || len(loads) != 1 {
		t.Fatalf("Expected the snapshot loads, got %v", item["loads"])
	}
	if loads[0].(map[string]interface{})["unit"] != "bw" {
		t.Errorf("Expected the snapshot to keep the bodyweight load, got %v", loads[0])
	}
}

func TestSessionHandler_CreateSession_SnapshotsTrainingWithoutProgram(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "presolo@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:  handler.NewSessionHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	trainingID, _ := createTestCoachTrainingWithItems(t, userToken, app)

	session := playSession(t, app, userToken, map[string]interface{}{"training_id": trainingID})

	prescription := sessionPrescription(t, session)
	if prescription["id"] != trainingID {
		t.Errorf("Expected the training id %s, got %v", trainingID, prescription["id"])
	}
	if _, present := prescription["program_session_id"]; present {
		t.Errorf("Expected no program_session_id on a training played outside a program, got %v", prescription["program_session_id"])
	}
	if len(prescriptionItems(t, prescription)) != 1 {
		t.Errorf("Expected the training items in the snapshot, got %v", prescription["items"])
	}
}

func TestSessionHandler_CreateSession_NoPrescriptionWithoutTraining(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "prenone@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"name":     "Free session",
		"notes":    "",
		"activity": 1,
		"duration": 300,
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, userToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d", resp.StatusCode)
	}
	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)
	if _, present := session["prescription"]; present {
		t.Errorf("Expected no prescription on a session run from nothing, got %v", session["prescription"])
	}
}

// prescribeSession puts one session on week 1 and returns its program session id.
func prescribeSession(t *testing.T, app *fiber.App, coachToken, userID, programID, trainingID string, overrides map[string]interface{}) string {
	t.Helper()
	session := map[string]interface{}{
		"training_id": trainingID,
		"day_of_week": 0,
	}
	if overrides != nil {
		session["overrides"] = overrides["overrides"]
	}
	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{session},
	})
	return weekSessionIDs(created)[0]
}

// The snapshot has to freeze every key an override may carry, not just the ones
// that reshape the hangboard grid. A timing the coach overrode and the athlete
// ran is part of what they were asked to do.
func TestSessionHandler_CreateSession_SnapshotsNonLayoutOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "prenonlayout")
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{
				"rest_seconds":        120,
				"hb_worktime_seconds": 10,
				"cycle_rest_seconds":  240,
			}},
		},
	})

	session := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})

	item := prescriptionItems(t, sessionPrescription(t, session))[0].(map[string]interface{})
	for _, field := range []struct {
		key  string
		want float64
	}{
		{"rest_seconds", 120},
		{"worktime_seconds", 10},
		{"cycle_rest_seconds", 240},
	} {
		if item[field.key] != field.want {
			t.Errorf("Expected the overridden %s %v in the snapshot, got %v", field.key, field.want, item[field.key])
		}
	}
}

// An override carrying an empty array prescribes nothing, so the clients leave
// the training value in place and the snapshot has to agree.
func TestSessionHandler_CreateSession_EmptyArrayOverrideKeepsTrainingValue(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "preempty")
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{"loads": []interface{}{}}},
		},
	})

	session := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})

	item := prescriptionItems(t, sessionPrescription(t, session))[0].(map[string]interface{})
	loads, ok := item["loads"].([]interface{})
	if !ok || len(loads) != 1 {
		t.Fatalf("Expected the training loads kept, got %v", item["loads"])
	}
}

// playSessionExpecting posts a played session and returns the raw response, so a
// test can assert on a refusal rather than on the session it would have created.
func playSessionExpecting(t *testing.T, app *fiber.App, userToken string, links map[string]interface{}) *http.Response {
	t.Helper()
	payload := map[string]interface{}{
		"name":     "Prescribed hangboard",
		"notes":    "",
		"activity": 0,
		"origin":   "played",
		"duration": 600,
	}
	for k, v := range links {
		payload[k] = v
	}
	body, _ := json.Marshal(payload)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, userToken))
	if err != nil {
		t.Fatalf("Failed to play session: %v", err)
	}
	return resp
}

// prescribeSessionOnWeek puts one session on the given week, so a test can make a
// second training reachable to the athlete without prescribing it for the week
// under test.
func prescribeSessionOnWeek(t *testing.T, app *fiber.App, coachToken, userID, programID, trainingID string, weekNum int) string {
	t.Helper()
	created := upsertWeekSessions(t, app, coachToken, userID, programID, weekNum, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
		},
	})
	return weekSessionIDs(created)[0]
}

// The program session decides the training, so a request pairing it with some
// other training the athlete can also reach is refused rather than freezing a
// prescription that was never given.
func TestSessionHandler_CreateSession_RejectsTrainingThatIsNotPrescribed(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "premismatch")
	prescribedID, _ := createTestCoachTrainingWithItems(t, coachToken, app)
	otherID, _ := createTestCoachTrainingWithItems(t, coachToken, app)

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, prescribedID, nil)
	// The other training is reachable too, since the same program prescribes it.
	prescribeSessionOnWeek(t, app, coachToken, userID, programID, otherID, 2)

	resp := playSessionExpecting(t, app, userToken, map[string]interface{}{
		"training_id":        otherID,
		"program_session_id": programSessionID,
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 playing a training the program session does not prescribe, got %d", resp.StatusCode)
	}
}

// A played session that names only its program session still freezes what that
// row prescribed. It locks the coach's week either way, so leaving it without a
// snapshot would lose the prescription outright.
func TestSessionHandler_CreateSession_SnapshotsFromProgramSessionAlone(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "prepsonly")
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
		},
	})

	session := playSession(t, app, userToken, map[string]interface{}{
		"program_session_id": programSessionID,
	})

	if session["training_id"] != trainingID {
		t.Errorf("Expected the session to adopt the prescribed training %s, got %v", trainingID, session["training_id"])
	}
	prescription := sessionPrescription(t, session)
	if prescription["id"] != trainingID {
		t.Errorf("Expected the prescribed training id %s, got %v", trainingID, prescription["id"])
	}
	item := prescriptionItems(t, prescription)[0].(map[string]interface{})
	if item["reps"] != float64(5) {
		t.Errorf("Expected the override merged into the snapshot, got %v", item["reps"])
	}
}

// The snapshot is a whole training per row, and only the detail screen reads it.
func TestSessionHandler_GetUserSessions_OmitsPrescription(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "prelist")
	trainingID, _ := createTestCoachTrainingWithItems(t, coachToken, app)

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, nil)
	created := playSession(t, app, userToken, map[string]interface{}{
		"program_session_id": programSessionID,
	})

	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, "/api/sessions", nil, userToken))
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 listing sessions, got %d", resp.StatusCode)
	}
	var list []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("Expected 1 session in the list, got %d", len(list))
	}
	if _, present := list[0]["prescription"]; present {
		t.Errorf("Expected no prescription on a list item, got %v", list[0]["prescription"])
	}

	// The detail endpoint still carries it, which is what the list defers to.
	sessionPrescription(t, getSessionJSON(t, app, userToken, created["id"].(string)))
}

// recordAssessment logs a session carrying one assessment result, which is how
// the athlete's results get on file.
func recordAssessment(t *testing.T, app *fiber.App, userToken string, assessmentID string, right, left float64, date string) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"name":          "Max hang test",
		"notes":         "",
		"activity":      0,
		"origin":        "logged",
		"duration":      300,
		"is_assessment": true,
		"date":          date,
		"assessments": []map[string]interface{}{
			{"assessment_id": assessmentID, "right_value": right, "left_value": left},
		},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, userToken))
	if err != nil {
		t.Fatalf("Failed to record assessment: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording assessment, got %d", resp.StatusCode)
	}
}

func prescriptionAssessments(t *testing.T, prescription map[string]interface{}) []interface{} {
	t.Helper()
	inputs, ok := prescription["resolved_against"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected resolved_against on the prescription, got %v", prescription["resolved_against"])
	}
	assessments, ok := inputs["assessments"].([]interface{})
	if !ok {
		t.Fatalf("Expected assessments under resolved_against, got %v", inputs["assessments"])
	}
	return assessments
}

// A load set as a percentage of an assessment is stored as the percentage, so
// the results it resolves against are as much part of the prescription as the
// percentage is. Reassessing must not restate what the athlete was asked for.
func TestSessionHandler_CreateSession_FreezesAssessmentResults(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "preassess")
	trainingID, _ := createTestCoachTrainingWithItems(t, coachToken, app)

	recordAssessment(t, app, userToken, testutil.BuiltinCriticalForceID, 50, 48, "2026-01-10T10:00:00Z")

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, nil)
	created := playSession(t, app, userToken, map[string]interface{}{
		"program_session_id": programSessionID,
	})

	assessments := prescriptionAssessments(t, sessionPrescription(t, created))
	if len(assessments) != 1 {
		t.Fatalf("Expected the athlete's one assessment frozen, got %v", assessments)
	}
	frozen := assessments[0].(map[string]interface{})
	if frozen["right_value"] != float64(50) || frozen["left_value"] != float64(48) {
		t.Fatalf("Expected the results as they stood, got %v", frozen)
	}

	// The athlete gets stronger. What they were asked to do does not change.
	recordAssessment(t, app, userToken, testutil.BuiltinCriticalForceID, 60, 58, "2026-02-10T10:00:00Z")

	reread := prescriptionAssessments(t, sessionPrescription(t, getSessionJSON(t, app, userToken, created["id"].(string))))
	stillFrozen := reread[0].(map[string]interface{})
	if stillFrozen["right_value"] != float64(50) || stillFrozen["left_value"] != float64(48) {
		t.Errorf("Expected the frozen results to survive a reassessment, got %v", stillFrozen)
	}
}

// An athlete who has assessed nothing still gets the block, empty, which is the
// case the clients read as "take the coach fallback".
func TestSessionHandler_CreateSession_FreezesEmptyAssessmentResults(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "prenoassess")
	trainingID, _ := createTestCoachTrainingWithItems(t, coachToken, app)

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, nil)
	created := playSession(t, app, userToken, map[string]interface{}{
		"program_session_id": programSessionID,
	})

	if assessments := prescriptionAssessments(t, sessionPrescription(t, created)); len(assessments) != 0 {
		t.Errorf("Expected no frozen assessments, got %v", assessments)
	}
}

// The four keys Krakoer/crimpy#51 added to the override reach the athlete the
// same way the timings do. A duration retimed, an emom clock moved, a rep count
// left open and a max effort turned into a number are all part of what the week
// asked for, so the snapshot has to carry them rather than the training values.
func TestSessionHandler_CreateSession_SnapshotsWidenedOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "prewiden")

	status, training := postJSON(t, app, "/api/trainings", coachToken, map[string]interface{}{
		"title": "Widened blocks",
		"items": []map[string]interface{}{
			{"type": "exercise", "duration": 30},
			{"type": "emom", "cycles": 10, "interval_seconds": 60, "items": []map[string]interface{}{
				{"type": "exercise", "reps": 5},
			}},
			{
				"type": "hangboard_rep", "worktime_seconds": 7, "hand": "both",
				"granularity": "uniform", "load_is_max": true,
				"loads": []map[string]interface{}{{"value": 0, "unit": "max"}},
			},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}
	trainingID := training["id"].(string)
	items := training["items"].([]interface{})
	plankID := items[0].(map[string]interface{})["id"].(string)
	emom := items[1].(map[string]interface{})
	emomID := emom["id"].(string)
	roundID := emom["items"].([]interface{})[0].(map[string]interface{})["id"].(string)
	hangID := items[2].(map[string]interface{})["id"].(string)

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": plankID, "overrides": map[string]interface{}{"duration": 45}},
			{"item_id": emomID, "overrides": map[string]interface{}{"interval_seconds": 90}},
			{"item_id": roundID, "overrides": map[string]interface{}{"reps_is_max": true}},
			{"item_id": hangID, "overrides": map[string]interface{}{
				"load_is_max": false,
				"loads":       []map[string]interface{}{{"value": 25, "unit": "kg"}},
			}},
		},
	})

	session := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})

	snapshot := prescriptionItems(t, sessionPrescription(t, session))
	plank := snapshot[0].(map[string]interface{})
	if plank["duration"] != float64(45) {
		t.Errorf("Expected the retimed duration 45 in the snapshot, got %v", plank["duration"])
	}
	emomSnapshot := snapshot[1].(map[string]interface{})
	if emomSnapshot["interval_seconds"] != float64(90) {
		t.Errorf("Expected the moved interval 90 in the snapshot, got %v", emomSnapshot["interval_seconds"])
	}
	round := emomSnapshot["items"].([]interface{})[0].(map[string]interface{})
	if round["reps_is_max"] != true {
		t.Errorf("Expected the rep count left open in the snapshot, got %v", round["reps_is_max"])
	}
	hang := snapshot[2].(map[string]interface{})
	if hang["load_is_max"] != false {
		t.Errorf("Expected the max effort marker cleared in the snapshot, got %v", hang["load_is_max"])
	}
}
