package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// createOpenTraining makes a training holding an AMRAP exercise inside an emom,
// and returns the training id with the id of each item, which is what an item
// result has to name to be kept.
func createOpenTraining(t *testing.T, app *fiber.App, token string) (trainingID, emomID, exerciseID string) {
	t.Helper()
	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Pull up EMOM",
		"items": []map[string]interface{}{
			{
				"type":             "emom",
				"cycles":           10,
				"interval_seconds": 60,
				"items": []map[string]interface{}{
					{"type": "exercise", "reps_is_max": true},
				},
			},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}
	emom := firstItem(t, training)
	children := emom["items"].([]interface{})
	return training["id"].(string), emom["id"].(string), children[0].(map[string]interface{})["id"].(string)
}

// postSessionWithItemResults plays a session from the training and returns the
// response, leaving the status assertion to the caller.
func postSessionWithItemResults(t *testing.T, app *fiber.App, token, trainingID string, results []map[string]interface{}) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"name":         "EMOM day",
		"notes":        "",
		"activity":     3,
		"origin":       "played",
		"duration":     420,
		"training_id":  trainingID,
		"item_results": results,
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, token))
	if err != nil {
		t.Fatalf("Failed to play session: %v", err)
	}
	return resp
}

func sessionItemResults(t *testing.T, app *fiber.App, token, sessionID string) []interface{} {
	t.Helper()
	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, "/api/sessions/"+sessionID, nil, token))
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 getting session, got %d", resp.StatusCode)
	}
	var detail map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&detail)
	results, ok := detail["item_results"].([]interface{})
	if !ok {
		t.Fatalf("Expected item_results on the session detail, got %v", detail["item_results"])
	}
	return results
}

func TestSessionItemResults_RecordsAndReadsBackOpenCounts(t *testing.T) {
	app, token := openItemsApp(t, "itemres1@test.com")
	trainingID, emomID, exerciseID := createOpenTraining(t, app, token)

	resp := postSessionWithItemResults(t, app, token, trainingID, []map[string]interface{}{
		{"training_item_id": exerciseID, "occurrence": 0, "field": "reps", "value": 23},
		{"training_item_id": exerciseID, "occurrence": 1, "field": "reps", "value": 18},
		{"training_item_id": emomID, "occurrence": 0, "field": "cycles", "value": 7},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 playing the session, got %d", resp.StatusCode)
	}
	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	results := sessionItemResults(t, app, token, session["id"].(string))
	if len(results) != 3 {
		t.Fatalf("Expected the three counts to be stored, got %v", results)
	}

	byKey := map[string]float64{}
	for _, raw := range results {
		r := raw.(map[string]interface{})
		key := r["training_item_id"].(string) + "/" + r["field"].(string)
		if r["occurrence"].(float64) == 1 {
			key += "/1"
		}
		byKey[key] = r["value"].(float64)
	}
	if byKey[exerciseID+"/reps"] != 23 {
		t.Errorf("Expected the first AMRAP to read back as 23, got %v", byKey[exerciseID+"/reps"])
	}
	if byKey[exerciseID+"/reps/1"] != 18 {
		t.Errorf("Expected the second pass to read back as 18, got %v", byKey[exerciseID+"/reps/1"])
	}
	if byKey[emomID+"/cycles"] != 7 {
		t.Errorf("Expected the emom to read back as 7 rounds, got %v", byKey[emomID+"/cycles"])
	}
}

// The item ids rotate whenever the coach edits the training mid run, so a count
// naming an item the frozen prescription does not hold is unreadable. It is
// dropped rather than costing the athlete the whole session.
func TestSessionItemResults_DropsResultOutsideThePrescription(t *testing.T) {
	app, token := openItemsApp(t, "itemres2@test.com")
	trainingID, _, exerciseID := createOpenTraining(t, app, token)

	resp := postSessionWithItemResults(t, app, token, trainingID, []map[string]interface{}{
		{"training_item_id": exerciseID, "occurrence": 0, "field": "reps", "value": 23},
		{"training_item_id": "11111111-1111-1111-1111-111111111111", "occurrence": 0, "field": "reps", "value": 9},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 playing the session, got %d", resp.StatusCode)
	}
	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	results := sessionItemResults(t, app, token, session["id"].(string))
	if len(results) != 1 {
		t.Fatalf("Expected only the result naming a prescribed item, got %v", results)
	}
	if results[0].(map[string]interface{})["training_item_id"] != exerciseID {
		t.Errorf("Expected the kept result to be the prescribed one, got %v", results[0])
	}
}

func TestSessionItemResults_RejectsInvalidInput(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"unknown field":  {"occurrence": 0, "field": "seconds", "value": 5},
		"negative value": {"occurrence": 0, "field": "reps", "value": -1},
		"malformed id":   {"training_item_id": "not-a-uuid", "occurrence": 0, "field": "reps", "value": 5},
	}
	for name, result := range cases {
		t.Run(name, func(t *testing.T) {
			app, token := openItemsApp(t, "itemresbad"+name[:4]+"@test.com")
			trainingID, _, exerciseID := createOpenTraining(t, app, token)
			if _, present := result["training_item_id"]; !present {
				result["training_item_id"] = exerciseID
			}

			resp := postSessionWithItemResults(t, app, token, trainingID, []map[string]interface{}{result})
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("Expected 400 for %s, got %d", name, resp.StatusCode)
			}
		})
	}
}

// The unique index would otherwise refuse the second row mid transaction, which
// costs a 500 rather than telling the client what it sent twice.
func TestSessionItemResults_RejectsTwoAnswersForOnePass(t *testing.T) {
	app, token := openItemsApp(t, "itemres3@test.com")
	trainingID, _, exerciseID := createOpenTraining(t, app, token)

	resp := postSessionWithItemResults(t, app, token, trainingID, []map[string]interface{}{
		{"training_item_id": exerciseID, "occurrence": 0, "field": "reps", "value": 23},
		{"training_item_id": exerciseID, "occurrence": 0, "field": "reps", "value": 24},
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for two counts answering one pass, got %d", resp.StatusCode)
	}
}

// The coach reads the counts off the same envelope the athlete does, which is
// what makes an AMRAP visible to the person who prescribed it.
func TestSessionItemResults_CoachReadsClientCounts(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "itemrescoach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "itemresuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:  handler.NewSessionHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		CoachHandler:    handler.NewCoachHandler(queries, pool),
	})

	trainingID, _, exerciseID := createOpenTraining(t, app, userToken)
	resp := postSessionWithItemResults(t, app, userToken, trainingID, []map[string]interface{}{
		{"training_item_id": exerciseID, "occurrence": 0, "field": "reps", "value": 23},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 playing the session, got %d", resp.StatusCode)
	}
	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	url := "/api/coach/clients/" + userID + "/sessions/" + session["id"].(string)
	read, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, url, nil, coachToken))
	if err != nil {
		t.Fatalf("Failed to read the client session: %v", err)
	}
	if read.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 reading the client session, got %d", read.StatusCode)
	}
	var detail map[string]interface{}
	json.NewDecoder(read.Body).Decode(&detail)

	results, ok := detail["item_results"].([]interface{})
	if !ok || len(results) != 1 {
		t.Fatalf("Expected the coach to see the recorded count, got %v", detail["item_results"])
	}
	if results[0].(map[string]interface{})["value"] != float64(23) {
		t.Errorf("Expected the coach to read 23 reps, got %v", results[0])
	}
}
