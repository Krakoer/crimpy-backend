package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// The force curve is what a critical force or an MVC result means, so it rides
// along with the assessment session that recorded it. Everything below drives
// the API rather than the store, since accepting the curve and handing it back
// on the detail read is the whole contract the app codes against.

func samplesRequest(isAssessment bool, samples interface{}) []byte {
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
	encoded, _ := json.Marshal(body)
	return encoded
}

func postSession(t *testing.T, app *fiber.App, token string, body []byte) (*http.Response, map[string]interface{}) {
	t.Helper()
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	var decoded map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&decoded)
	return resp, decoded
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
	resp, created := postSession(t, app, token, samplesRequest(true, curve))
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusCreated, resp.StatusCode, created)
	}

	sessionID, _ := created["id"].(string)
	if nested, ok := created["session"].(map[string]interface{}); ok {
		sessionID, _ = nested["id"].(string)
	}
	if sessionID == "" {
		t.Fatalf("Create returned no session id: %v", created)
	}

	req := testutil.NewJSONRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	detail, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to read the session back: %v", err)
	}
	if detail.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected status %d reading back, got %d", fiber.StatusOK, detail.StatusCode)
	}

	var read map[string]interface{}
	json.NewDecoder(detail.Body).Decode(&read)
	session, ok := read["session"].(map[string]interface{})
	if !ok {
		session = read
	}
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
	resp, decoded := postSession(t, app, token, samplesRequest(false, curve))

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusBadRequest, resp.StatusCode, decoded)
	}
}

func TestSessionSamples_RefusedWhenTheArraysDisagree(t *testing.T) {
	app, token := samplesApp(t, "samples-mismatch@test.com")

	curve := map[string]interface{}{
		"t0": "2026-08-27T09:12:03Z",
		"ms": []int32{0, 125},
		"kg": []float32{12},
	}
	resp, decoded := postSession(t, app, token, samplesRequest(true, curve))

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusBadRequest, resp.StatusCode, decoded)
	}
}

func TestSessionSamples_RefusedWhenT0IsNotADate(t *testing.T) {
	app, token := samplesApp(t, "samples-badt0@test.com")

	curve := map[string]interface{}{
		"t0": "not a date",
		"ms": []int32{0},
		"kg": []float32{12},
	}
	resp, decoded := postSession(t, app, token, samplesRequest(true, curve))

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusBadRequest, resp.StatusCode, decoded)
	}
}

// An assessment session that recorded nothing is ordinary, not an error.
func TestSessionSamples_AbsentWhenNoneWereSent(t *testing.T) {
	app, token := samplesApp(t, "samples-none@test.com")

	resp, created := postSession(t, app, token, samplesRequest(true, nil))
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusCreated, resp.StatusCode, created)
	}

	session, ok := created["session"].(map[string]interface{})
	if !ok {
		session = created
	}
	if _, present := session["samples"]; present {
		t.Errorf("Expected no samples key, got %v", session["samples"])
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
	if resp, decoded := postSession(t, app, token, samplesRequest(true, curve)); resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d: %v", fiber.StatusCreated, resp.StatusCode, decoded)
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
