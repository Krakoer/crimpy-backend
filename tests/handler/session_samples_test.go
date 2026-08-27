package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// The force curve is what a critical force or an MVC result means, so it rides
// along with the assessment session that recorded it. Everything below drives
// the API rather than the store, since accepting the curve and handing it back
// on the detail read is the whole contract the app codes against.

func samplesRequest(isAssessment bool, samples interface{}) map[string]interface{} {
	body := map[string]interface{}{
		"name":          "Critical force",
		"notes":         "",
		"is_assessment": isAssessment,
		"activity":      0,
		"origin":        "played",
		"duration":      240,
	}
	if samples != nil {
		body["samples"] = samples
	}
	return body
}

func samplesApp(t *testing.T, email string) (*fiber.App, string) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	_, token := testutil.CreateTestUser(t, queries, email)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
	})
	return app, token
}

func TestSessionSamples_StoredAndReadBack(t *testing.T) {
	app, token := samplesApp(t, "samples-roundtrip@test.com")

	curve := map[string]interface{}{
		"t0": "2026-08-27T09:12:03Z",
		"ms": []int32{0, 125, 250},
		"kg": []float32{0, 12.5, 31.25},
	}
	status, created := postJSON(t, app, "/api/sessions", token, samplesRequest(true, curve))
	if status != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusCreated, status, created)
	}

	sessionID, _ := created["id"].(string)
	if sessionID == "" {
		t.Fatalf("Create returned no session id: %v", created)
	}

	session := getSessionJSON(t, app, token, sessionID)
	samples, ok := session["samples"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected the curve back on the detail read, got %v", session["samples"])
	}
	if samples["t0"] != "2026-08-27T09:12:03Z" {
		t.Errorf("Expected t0 to survive, got %v", samples["t0"])
	}
	if got := len(samples["ms"].([]interface{})); got != 3 {
		t.Errorf("Expected 3 offsets, got %d", got)
	}
	if got := samples["kg"].([]interface{})[2]; got != 31.25 {
		t.Errorf("Expected the last reading to survive, got %v", got)
	}
}

// The column is for assessments and the check constraint says so, but a request
// carrying a curve on an ordinary session should be told why rather than hitting
// the constraint and coming back a 500.
func TestSessionSamples_RefusedOnAnOrdinarySession(t *testing.T) {
	app, token := samplesApp(t, "samples-notassessment@test.com")

	curve := map[string]interface{}{
		"t0": "2026-08-27T09:12:03Z",
		"ms": []int32{0},
		"kg": []float32{12},
	}
	status, decoded := postJSON(t, app, "/api/sessions", token, samplesRequest(false, curve))

	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusBadRequest, status, decoded)
	}
}

func TestSessionSamples_RefusedWhenTheArraysDisagree(t *testing.T) {
	app, token := samplesApp(t, "samples-mismatch@test.com")

	curve := map[string]interface{}{
		"t0": "2026-08-27T09:12:03Z",
		"ms": []int32{0, 125},
		"kg": []float32{12},
	}
	status, decoded := postJSON(t, app, "/api/sessions", token, samplesRequest(true, curve))

	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusBadRequest, status, decoded)
	}
}

func TestSessionSamples_RefusedWhenT0IsNotADate(t *testing.T) {
	app, token := samplesApp(t, "samples-badt0@test.com")

	curve := map[string]interface{}{
		"t0": "not a date",
		"ms": []int32{0},
		"kg": []float32{12},
	}
	status, decoded := postJSON(t, app, "/api/sessions", token, samplesRequest(true, curve))

	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusBadRequest, status, decoded)
	}
}

// An assessment session that recorded nothing is ordinary, not an error.
func TestSessionSamples_AbsentWhenNoneWereSent(t *testing.T) {
	app, token := samplesApp(t, "samples-none@test.com")

	status, created := postJSON(t, app, "/api/sessions", token, samplesRequest(true, nil))
	if status != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusCreated, status, created)
	}

	if _, present := created["samples"]; present {
		t.Errorf("Expected no samples key, got %v", created["samples"])
	}
}

// The cap is the only thing standing between one request and an unbounded
// document in the row, so it is worth a test even though the app's own buffer
// stops well short of it.
func TestSessionSamples_RefusedPastTheCap(t *testing.T) {
	app, token := samplesApp(t, "samples-cap@test.com")

	const tooMany = 60001
	offsets := make([]int32, tooMany)
	readings := make([]float32, tooMany)
	for i := range offsets {
		offsets[i] = int32(i * 125)
		readings[i] = 12.5
	}
	curve := map[string]interface{}{
		"t0": "2026-08-27T09:12:03Z",
		"ms": offsets,
		"kg": readings,
	}
	status, decoded := postJSON(t, app, "/api/sessions", token, samplesRequest(true, curve))

	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusBadRequest, status, decoded)
	}
}

// The history list is read on every open and a curve is the bulkiest thing a
// session holds, so it must stay off the list the way the prescription does.
func TestSessionSamples_LeftOffTheList(t *testing.T) {
	app, token := samplesApp(t, "samples-list@test.com")

	curve := map[string]interface{}{
		"t0": "2026-08-27T09:12:03Z",
		"ms": []int32{0, 125},
		"kg": []float32{0, 12.5},
	}
	if status, decoded := postJSON(t, app, "/api/sessions", token, samplesRequest(true, curve)); status != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusCreated, status, decoded)
	}

	req := testutil.NewJSONRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	var list []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("Expected one session, got %d", len(list))
	}
	if _, present := list[0]["samples"]; present {
		t.Errorf("Expected the list to leave the curve out, got %v", list[0]["samples"])
	}
}

// A rename answers with the session, and the curve is the bulkiest thing it
// holds. The update never touches it, so shipping it back buys the client
// nothing.
func TestSessionSamples_LeftOffAnUpdate(t *testing.T) {
	app, token := samplesApp(t, "samples-update@test.com")

	curve := map[string]interface{}{
		"t0": "2026-08-27T09:12:03Z",
		"ms": []int32{0, 125},
		"kg": []float32{0, 12.5},
	}
	status, created := postJSON(t, app, "/api/sessions", token, samplesRequest(true, curve))
	if status != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusCreated, status, created)
	}
	sessionID, _ := created["id"].(string)

	body, _ := json.Marshal(map[string]interface{}{
		"name":     "Renamed",
		"notes":    "",
		"duration": 240,
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/sessions/"+sessionID, body, token))
	if err != nil {
		t.Fatalf("Failed to update the session: %v", err)
	}
	var updated map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updated)
	if _, present := updated["samples"]; present {
		t.Errorf("Expected the update to leave the curve out, got %v", updated["samples"])
	}

	// Left off the response, still in the row.
	if _, ok := getSessionJSON(t, app, token, sessionID)["samples"].(map[string]interface{}); !ok {
		t.Errorf("Expected the stored curve to survive the update")
	}
}
