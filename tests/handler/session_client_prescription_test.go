package handler_test

import (
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// generatedPrescription is what a client sends for a run of a training the
// server cannot read: one Crimpy generates on the device. Its items are named
// by keys the client minted, which is the whole reason the two link columns are
// text rather than uuid.
func generatedPrescription(itemIDs ...string) map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(itemIDs))
	for i, id := range itemIDs {
		items = append(items, map[string]interface{}{
			"id":               id,
			"type":             "repeater",
			"position":         i,
			"cycles":           4,
			"reps":             6,
			"worktime_seconds": 7,
		})
	}
	return map[string]interface{}{
		"id":            "builtin-max-hangs",
		"title":         "Max hangs",
		"training_type": "hangboard",
		"items":         items,
	}
}

// postGeneratedSession plays a session from a client-supplied prescription and
// hands back the raw response, leaving the status assertion to the caller.
func postGeneratedSession(t *testing.T, app *fiber.App, token string, body map[string]interface{}) *http.Response {
	t.Helper()
	payload := map[string]interface{}{
		"name":     "Max hangs",
		"notes":    "",
		"activity": 3,
		"origin":   "played",
		"duration": 600,
	}
	for k, v := range body {
		payload[k] = v
	}
	encoded, _ := json.Marshal(payload)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", encoded, token))
	if err != nil {
		t.Fatalf("Failed to play session: %v", err)
	}
	return resp
}

// getSessionDetail reads a session back the way the app does, so a test can
// assert on what was stored rather than on what was echoed by the create.
func getSessionDetail(t *testing.T, app *fiber.App, token, sessionID string) map[string]interface{} {
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
	return detail
}

func createdSessionID(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp.StatusCode != fiber.StatusCreated {
		var body map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("Expected 201 playing the session, got %d: %v", resp.StatusCode, body)
	}
	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	if session, ok := created["session"].(map[string]interface{}); ok {
		return session["id"].(string)
	}
	return created["id"].(string)
}

// What #111 is about: before this, a run of a training Crimpy generates could
// carry no report at all, because the server had no prescription to key one
// into and the column took none but a uuid.
func TestClientPrescription_KeepsReportsOnAGeneratedRun(t *testing.T) {
	app, token := openItemsApp(t, "clientpres1@test.com")

	resp := postGeneratedSession(t, app, token, map[string]interface{}{
		"prescription": generatedPrescription("builtin:max-hangs:0", "builtin:max-hangs:1"),
		"item_results": []map[string]interface{}{
			{"training_item_id": "builtin:max-hangs:0", "occurrence": 0, "reps": 5, "note": "right hand slipped on the last one"},
			{"training_item_id": "builtin:max-hangs:1", "occurrence": 0, "cycles": 3},
		},
	})
	sessionID := createdSessionID(t, resp)

	byPass := resultsByPass(sessionItemResults(t, app, token, sessionID))
	first, ok := byPass["builtin:max-hangs:0/0"]
	if !ok {
		t.Fatalf("Expected a report on the first generated step, got %v", byPass)
	}
	if first["reps"] != float64(5) {
		t.Errorf("Expected 5 reps reported, got %v", first["reps"])
	}
	if first["note"] != "right hand slipped on the last one" {
		t.Errorf("Expected the note to be kept, got %v", first["note"])
	}
	if second, ok := byPass["builtin:max-hangs:1/0"]; !ok || second["cycles"] != float64(3) {
		t.Errorf("Expected 3 rounds on the second step, got %v", byPass)
	}
}

// The prescription is what the reports are read against later, so it has to
// come back as the client wrote it, keys included.
func TestClientPrescription_IsReadBackOnTheSession(t *testing.T) {
	app, token := openItemsApp(t, "clientpres2@test.com")

	resp := postGeneratedSession(t, app, token, map[string]interface{}{
		"prescription": generatedPrescription("builtin:max-hangs:0"),
	})
	sessionID := createdSessionID(t, resp)

	session := getSessionDetail(t, app, token, sessionID)["session"].(map[string]interface{})
	prescription, ok := session["prescription"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected the prescription on the session detail, got %v", session["prescription"])
	}
	items, ok := prescription["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("Expected one prescribed item, got %v", prescription["items"])
	}
	if id := items[0].(map[string]interface{})["id"]; id != "builtin:max-hangs:0" {
		t.Errorf("Expected the generated key to survive, got %v", id)
	}
	if session["training_id"] != nil {
		t.Errorf("Expected no training on a generated run, got %v", session["training_id"])
	}
}

// A rep names its step the same way a report does, so a generated run reads
// block by block rather than as one pooled list.
func TestClientPrescription_LinksRepsToGeneratedSteps(t *testing.T) {
	app, token := openItemsApp(t, "clientpres3@test.com")

	resp := postGeneratedSession(t, app, token, map[string]interface{}{
		"prescription": generatedPrescription("builtin:max-hangs:0"),
		"rep_datas": []map[string]interface{}{
			{
				"average_weight": 32.5, "is_rest": false, "hand": "right",
				"duration": 7, "target_weight": 34, "index": 0,
				"grip_position": 0, "training_item_id": "builtin:max-hangs:0",
			},
		},
	})
	sessionID := createdSessionID(t, resp)

	detail := getSessionDetail(t, app, token, sessionID)
	reps, ok := detail["rep_datas"].([]interface{})
	if !ok || len(reps) != 1 {
		t.Fatalf("Expected one rep on the session, got %v", detail["rep_datas"])
	}
	if id := reps[0].(map[string]interface{})["training_item_id"]; id != "builtin:max-hangs:0" {
		t.Errorf("Expected the rep to name the generated step, got %v", id)
	}
}

// A report the prescription cannot place is dropped rather than losing the
// session, the same as one naming an item a coach deleted mid run.
func TestClientPrescription_DropsAReportNamingAnItemItDoesNotHold(t *testing.T) {
	app, token := openItemsApp(t, "clientpres4@test.com")

	resp := postGeneratedSession(t, app, token, map[string]interface{}{
		"prescription": generatedPrescription("builtin:max-hangs:0"),
		"item_results": []map[string]interface{}{
			{"training_item_id": "builtin:max-hangs:0", "occurrence": 0, "reps": 5},
			{"training_item_id": "builtin:other:9", "occurrence": 0, "reps": 4},
		},
	})
	sessionID := createdSessionID(t, resp)

	results := sessionItemResults(t, app, token, sessionID)
	if len(results) != 1 {
		t.Fatalf("Expected the unplaceable report to be dropped, got %v", results)
	}
}

// The server writes its own snapshot whenever it can read the training, so a
// client copy alongside one is a second opinion about what was prescribed and
// is refused rather than silently ignored.
func TestClientPrescription_IsRefusedAlongsideATraining(t *testing.T) {
	app, token := openItemsApp(t, "clientpres5@test.com")
	trainingID, _, _ := createOpenTraining(t, app, token)

	resp := postGeneratedSession(t, app, token, map[string]interface{}{
		"training_id":  trainingID,
		"prescription": generatedPrescription("builtin:max-hangs:0"),
	})

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a prescription sent with a training, got %d", resp.StatusCode)
	}
}

// Without a prescription there is nothing for a report to name, so the report
// is dropped exactly as it was before this ticket.
func TestClientPrescription_ReportsNeedOne(t *testing.T) {
	app, token := openItemsApp(t, "clientpres6@test.com")

	resp := postGeneratedSession(t, app, token, map[string]interface{}{
		"item_results": []map[string]interface{}{
			{"training_item_id": "builtin:max-hangs:0", "occurrence": 0, "reps": 5},
		},
	})
	sessionID := createdSessionID(t, resp)

	if results := sessionItemResults(t, app, token, sessionID); len(results) != 0 {
		t.Fatalf("Expected no report to be kept without a prescription, got %v", results)
	}
}

func TestClientPrescription_RefusesAnItemWithNoID(t *testing.T) {
	app, token := openItemsApp(t, "clientpres7@test.com")

	prescription := generatedPrescription("builtin:max-hangs:0")
	prescription["items"].([]map[string]interface{})[0]["id"] = ""

	resp := postGeneratedSession(t, app, token, map[string]interface{}{
		"prescription": prescription,
	})

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for an unnamed prescribed item, got %d", resp.StatusCode)
	}
}

func TestClientPrescription_RefusesAnItemIDTheColumnCannotHold(t *testing.T) {
	app, token := openItemsApp(t, "clientpres8@test.com")

	resp := postGeneratedSession(t, app, token, map[string]interface{}{
		"prescription": generatedPrescription(strings.Repeat("k", 201)),
	})

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for an overlong item id, got %d", resp.StatusCode)
	}
}

func TestClientPrescription_RefusesAPrescriptionWithNoItems(t *testing.T) {
	app, token := openItemsApp(t, "clientpres9@test.com")

	resp := postGeneratedSession(t, app, token, map[string]interface{}{
		"prescription": generatedPrescription(),
	})

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a prescription holding no items, got %d", resp.StatusCode)
	}
}

// A session belongs to the athlete who played it, generated prescription or
// not: another user reading it gets nothing.
func TestClientPrescription_SessionStaysPrivateToItsAthlete(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	_, ownerToken := testutil.CreateTestUser(t, queries, "clientpres10@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "clientpres11@test.com")
	app := assessmentApp(t, pool, queries)

	resp := postGeneratedSession(t, app, ownerToken, map[string]interface{}{
		"prescription": generatedPrescription("builtin:max-hangs:0"),
		"item_results": []map[string]interface{}{
			{"training_item_id": "builtin:max-hangs:0", "occurrence": 0, "note": "felt strong"},
		},
	})
	sessionID := createdSessionID(t, resp)

	read, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, "/api/sessions/"+sessionID, nil, otherToken))
	if err != nil {
		t.Fatalf("Failed to read the session as another user: %v", err)
	}
	if read.StatusCode == fiber.StatusOK {
		t.Fatalf("Expected another user to be refused the session, got %d", read.StatusCode)
	}
}
