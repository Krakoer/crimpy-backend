package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// resultsByPass keys the stored results the way the unique constraint does, so
// a test can name one pass through one item and read every field off it.
func resultsByPass(results []interface{}) map[string]map[string]interface{} {
	byPass := map[string]map[string]interface{}{}
	for _, raw := range results {
		r := raw.(map[string]interface{})
		key := fmt.Sprintf("%s/%v", r["training_item_id"], r["occurrence"])
		byPass[key] = r
	}
	return byPass
}

func TestSessionItemResults_RecordsAndReadsBackOpenCounts(t *testing.T) {
	app, token := openItemsApp(t, "itemres1@test.com")
	trainingID, emomID, exerciseID := createOpenTraining(t, app, token)

	resp := postSessionWithItemResults(t, app, token, trainingID, []map[string]interface{}{
		{"training_item_id": exerciseID, "occurrence": 0, "reps": 23},
		{"training_item_id": exerciseID, "occurrence": 1, "reps": 18},
		{"training_item_id": emomID, "occurrence": 0, "cycles": 7},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 playing the session, got %d", resp.StatusCode)
	}
	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	results := sessionItemResults(t, app, token, session["id"].(string))
	if len(results) != 3 {
		t.Fatalf("Expected the three reports to be stored, got %v", results)
	}

	byPass := resultsByPass(results)
	if got := byPass[exerciseID+"/0"]["reps"]; got != float64(23) {
		t.Errorf("Expected the first AMRAP to read back as 23, got %v", got)
	}
	if got := byPass[exerciseID+"/1"]["reps"]; got != float64(18) {
		t.Errorf("Expected the second pass to read back as 18, got %v", got)
	}
	if got := byPass[emomID+"/0"]["cycles"]; got != float64(7) {
		t.Errorf("Expected the emom to read back as 7 rounds, got %v", got)
	}
}

// The point of the issue this shape came from: an ordinary exercise the sensor
// never sees carries what the athlete actually did and what they wrote about
// it, all on the one row that names the pass.
func TestSessionItemResults_RecordsNoteLoadAndDurationOnAnyItem(t *testing.T) {
	app, token := openItemsApp(t, "itemres4@test.com")
	trainingID, _, exerciseID := createOpenTraining(t, app, token)

	resp := postSessionWithItemResults(t, app, token, trainingID, []map[string]interface{}{
		{
			"training_item_id": exerciseID,
			"occurrence":       0,
			"reps":             8,
			"load_kg":          17.5,
			"duration_seconds": 42,
			"note":             "  failed at 8 reps on the last set but no pain  ",
		},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 playing the session, got %d", resp.StatusCode)
	}
	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	results := sessionItemResults(t, app, token, session["id"].(string))
	if len(results) != 1 {
		t.Fatalf("Expected the one report to be stored, got %v", results)
	}
	stored := results[0].(map[string]interface{})
	if stored["reps"] != float64(8) {
		t.Errorf("Expected 8 reps, got %v", stored["reps"])
	}
	if stored["load_kg"] != 17.5 {
		t.Errorf("Expected 17.5 kg, got %v", stored["load_kg"])
	}
	if stored["duration_seconds"] != float64(42) {
		t.Errorf("Expected 42 seconds, got %v", stored["duration_seconds"])
	}
	if stored["note"] != "failed at 8 reps on the last set but no pain" {
		t.Errorf("Expected the note trimmed and stored, got %v", stored["note"])
	}
}

// A field the athlete said nothing about is absent rather than zero, so a coach
// is never shown a number that was never reported.
func TestSessionItemResults_OmitsFieldsTheAthleteLeftAlone(t *testing.T) {
	app, token := openItemsApp(t, "itemres5@test.com")
	trainingID, _, exerciseID := createOpenTraining(t, app, token)

	resp := postSessionWithItemResults(t, app, token, trainingID, []map[string]interface{}{
		{"training_item_id": exerciseID, "occurrence": 0, "note": "did it with a band"},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 playing the session, got %d", resp.StatusCode)
	}
	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	stored := sessionItemResults(t, app, token, session["id"].(string))[0].(map[string]interface{})
	for _, field := range []string{"reps", "cycles", "load_kg", "duration_seconds"} {
		if _, present := stored[field]; present {
			t.Errorf("Expected %s to be absent on a report that only carried a note, got %v", field, stored[field])
		}
	}
}

// An athlete who opens a note field and types nothing in it reports nothing,
// which is a blank row rather than an error worth losing the session over.
func TestSessionItemResults_DropsAReportThatSaysNothing(t *testing.T) {
	app, token := openItemsApp(t, "itemres6@test.com")
	trainingID, _, exerciseID := createOpenTraining(t, app, token)

	resp := postSessionWithItemResults(t, app, token, trainingID, []map[string]interface{}{
		{"training_item_id": exerciseID, "occurrence": 0, "note": "   "},
		{"training_item_id": exerciseID, "occurrence": 1, "reps": 12},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 playing the session, got %d", resp.StatusCode)
	}
	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	results := sessionItemResults(t, app, token, session["id"].(string))
	if len(results) != 1 {
		t.Fatalf("Expected only the report that said something, got %v", results)
	}
	if results[0].(map[string]interface{})["reps"] != float64(12) {
		t.Errorf("Expected the kept report to be the rep count, got %v", results[0])
	}
}

// An item the coach deleted mid run leaves the athlete holding a count the
// frozen prescription cannot place, and an unreadable count is dropped rather
// than costing the athlete the whole session.
func TestSessionItemResults_DropsResultOutsideThePrescription(t *testing.T) {
	app, token := openItemsApp(t, "itemres2@test.com")
	trainingID, _, exerciseID := createOpenTraining(t, app, token)

	resp := postSessionWithItemResults(t, app, token, trainingID, []map[string]interface{}{
		{"training_item_id": exerciseID, "occurrence": 0, "reps": 23},
		{"training_item_id": "11111111-1111-1111-1111-111111111111", "occurrence": 0, "reps": 9},
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
		"negative reps":     {"occurrence": 0, "reps": -1},
		"negative cycles":   {"occurrence": 0, "cycles": -1},
		"negative load":     {"occurrence": 0, "load_kg": -0.5},
		"negative duration": {"occurrence": 0, "duration_seconds": -1},
		"overlong note":     {"occurrence": 0, "note": strings.Repeat("a", 2001)},
		"malformed id":      {"training_item_id": "not-a-uuid", "occurrence": 0, "reps": 5},
	}
	// The email has to differ per case, since the cleanup deletes on it, and
	// several of the names share their first words.
	index := 0
	for name, result := range cases {
		index++
		email := fmt.Sprintf("itemresbad%d@test.com", index)
		t.Run(name, func(t *testing.T) {
			app, token := openItemsApp(t, email)
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
		{"training_item_id": exerciseID, "occurrence": 0, "reps": 23},
		{"training_item_id": exerciseID, "occurrence": 0, "note": "and a second line about it"},
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for two reports answering one pass, got %d", resp.StatusCode)
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
		{"training_item_id": exerciseID, "occurrence": 0, "reps": 23, "note": "hard on the shoulders"},
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
	if results[0].(map[string]interface{})["reps"] != float64(23) {
		t.Errorf("Expected the coach to read 23 reps, got %v", results[0])
	}
	if results[0].(map[string]interface{})["note"] != "hard on the shoulders" {
		t.Errorf("Expected the coach to read the athlete note, got %v", results[0])
	}
}
