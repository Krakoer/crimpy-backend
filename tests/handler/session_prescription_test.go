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
			"type":             "hangboard_rep",
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
