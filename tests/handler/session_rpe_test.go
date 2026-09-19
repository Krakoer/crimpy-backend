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

// rpeApp stands a session handler up for the RPE cases, which all need the same
// user and the same two endpoints.
func rpeApp(t *testing.T, email string) (*fiber.App, string) {
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

func createRPESession(t *testing.T, app *fiber.App, token string, extra map[string]interface{}) map[string]interface{} {
	t.Helper()

	reqBody := map[string]interface{}{
		"name":     "RPE session",
		"notes":    "",
		"activity": 1,
		"duration": 3600,
	}
	for k, v := range extra {
		reqBody[k] = v
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected status %d creating the session, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	return created
}

func updateRPESession(t *testing.T, app *fiber.App, token, sessionID string, updateBody map[string]interface{}) (*http.Response, map[string]interface{}) {
	t.Helper()

	body, _ := json.Marshal(updateBody)
	req := testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/sessions/%s", sessionID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	var updated map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updated)
	return resp, updated
}

func TestSessionHandler_CreateSession_StoresRPE(t *testing.T) {
	app, token := rpeApp(t, "rpe-create@test.com")

	created := createRPESession(t, app, token, map[string]interface{}{"rpe": 8})

	if created["rpe"] != float64(8) {
		t.Errorf("Expected rpe 8, got %v", created["rpe"])
	}
	if created["rpe_failed"] != false {
		t.Errorf("Expected rpe_failed false, got %v", created["rpe_failed"])
	}
}

func TestSessionHandler_CreateSession_StoresRPEFailure(t *testing.T) {
	app, token := rpeApp(t, "rpe-echec@test.com")

	created := createRPESession(t, app, token, map[string]interface{}{"rpe_failed": true})

	if _, ok := created["rpe"]; ok {
		t.Errorf("Expected no rpe beside a failure, got %v", created["rpe"])
	}
	if created["rpe_failed"] != true {
		t.Errorf("Expected rpe_failed true, got %v", created["rpe_failed"])
	}
}

func TestSessionHandler_CreateSession_OmittedRPEIsAbsent(t *testing.T) {
	app, token := rpeApp(t, "rpe-absent@test.com")

	created := createRPESession(t, app, token, nil)

	if _, ok := created["rpe"]; ok {
		t.Errorf("Expected no rpe on a session that reported none, got %v", created["rpe"])
	}
	if created["rpe_failed"] != false {
		t.Errorf("Expected rpe_failed false, got %v", created["rpe_failed"])
	}
}

func TestSessionHandler_CreateSession_RejectsRPEOffTheScale(t *testing.T) {
	app, token := rpeApp(t, "rpe-range@test.com")

	for _, value := range []int{0, 4, 11} {
		reqBody := map[string]interface{}{
			"name":     "Off the scale",
			"notes":    "",
			"activity": 1,
			"duration": 60,
			"rpe":      value,
		}
		body, _ := json.Marshal(reqBody)
		req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
		req.Header.Set("Authorization", testutil.GetAuthHeader(token))

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to execute request: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Expected status %d for rpe %d, got %d", fiber.StatusBadRequest, value, resp.StatusCode)
		}
	}
}

func TestSessionHandler_CreateSession_RejectsRPEBesideFailure(t *testing.T) {
	app, token := rpeApp(t, "rpe-exclusive@test.com")

	reqBody := map[string]interface{}{
		"name":       "Both at once",
		"notes":      "",
		"activity":   1,
		"duration":   60,
		"rpe":        9,
		"rpe_failed": true,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

// The athlete who forgets the prompt is the normal case, so the update path has
// to be able to fill an RPE in long after the session.
func TestSessionHandler_UpdateSession_AddsRPEAfterTheFact(t *testing.T) {
	app, token := rpeApp(t, "rpe-later@test.com")

	created := createRPESession(t, app, token, nil)
	sessionID := created["id"].(string)

	resp, updated := updateRPESession(t, app, token, sessionID, map[string]interface{}{
		"name":     "RPE session",
		"notes":    "",
		"duration": 3600,
		"rpe":      7,
	})

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}
	if updated["rpe"] != float64(7) {
		t.Errorf("Expected rpe 7, got %v", updated["rpe"])
	}
}

func TestSessionHandler_UpdateSession_KeepsRPEWhenOmitted(t *testing.T) {
	app, token := rpeApp(t, "rpe-keep@test.com")

	created := createRPESession(t, app, token, map[string]interface{}{"rpe": 9})
	sessionID := created["id"].(string)

	_, updated := updateRPESession(t, app, token, sessionID, map[string]interface{}{
		"name":     "Renamed",
		"notes":    "Still nine",
		"duration": 3600,
	})

	if updated["rpe"] != float64(9) {
		t.Errorf("Expected the stored rpe 9 to survive an update that did not mention it, got %v", updated["rpe"])
	}
}

func TestSessionHandler_UpdateSession_ReplacesRPEWithFailure(t *testing.T) {
	app, token := rpeApp(t, "rpe-replace@test.com")

	created := createRPESession(t, app, token, map[string]interface{}{"rpe": 6})
	sessionID := created["id"].(string)

	_, updated := updateRPESession(t, app, token, sessionID, map[string]interface{}{
		"name":       "RPE session",
		"notes":      "",
		"duration":   3600,
		"rpe_failed": true,
	})

	if _, ok := updated["rpe"]; ok {
		t.Errorf("Expected the number to be dropped by a failure, got %v", updated["rpe"])
	}
	if updated["rpe_failed"] != true {
		t.Errorf("Expected rpe_failed true, got %v", updated["rpe_failed"])
	}
}

// Taking the answer back entirely: rpe_failed false on its own is what says the
// session is unrated again, since neither field means "leave it alone".
func TestSessionHandler_UpdateSession_ClearsRPE(t *testing.T) {
	app, token := rpeApp(t, "rpe-clear@test.com")

	created := createRPESession(t, app, token, map[string]interface{}{"rpe": 10})
	sessionID := created["id"].(string)

	_, updated := updateRPESession(t, app, token, sessionID, map[string]interface{}{
		"name":       "RPE session",
		"notes":      "",
		"duration":   3600,
		"rpe_failed": false,
	})

	if _, ok := updated["rpe"]; ok {
		t.Errorf("Expected the rpe to be cleared, got %v", updated["rpe"])
	}
	if updated["rpe_failed"] != false {
		t.Errorf("Expected rpe_failed false, got %v", updated["rpe_failed"])
	}
}

func TestSessionHandler_UpdateSession_RejectsRPEOffTheScale(t *testing.T) {
	app, token := rpeApp(t, "rpe-update-range@test.com")

	created := createRPESession(t, app, token, map[string]interface{}{"rpe": 8})
	sessionID := created["id"].(string)

	resp, _ := updateRPESession(t, app, token, sessionID, map[string]interface{}{
		"name":     "RPE session",
		"notes":    "",
		"duration": 3600,
		"rpe":      3,
	})

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

// The RPE pair is the only optional half of the update body, so a request that
// tries to send it alone is refused rather than blanking the session it meant
// to fill an answer in on.
func TestSessionHandler_UpdateSession_RejectsRPEWithoutTheSession(t *testing.T) {
	app, token := rpeApp(t, "rpe-partial@test.com")

	created := createRPESession(t, app, token, nil)
	sessionID := created["id"].(string)

	resp, _ := updateRPESession(t, app, token, sessionID, map[string]interface{}{"rpe": 7})

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}

	req := testutil.NewJSONRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	readBack, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to read the session back: %v", err)
	}

	var detail map[string]interface{}
	json.NewDecoder(readBack.Body).Decode(&detail)
	session := detail["session"].(map[string]interface{})
	if session["name"] != "RPE session" {
		t.Errorf("Expected the stored name to survive a refused update, got %v", session["name"])
	}
	if session["duration"] != float64(3600) {
		t.Errorf("Expected the stored duration to survive a refused update, got %v", session["duration"])
	}
}

func TestSessionHandler_UpdateSession_RejectsNegativeDuration(t *testing.T) {
	app, token := rpeApp(t, "rpe-duration@test.com")

	created := createRPESession(t, app, token, nil)
	sessionID := created["id"].(string)

	resp, _ := updateRPESession(t, app, token, sessionID, map[string]interface{}{
		"name":     "RPE session",
		"notes":    "",
		"duration": -1,
		"rpe":      7,
	})

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

func TestSessionHandler_UpdateSession_RPEUserIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, ownerToken := testutil.CreateTestUser(t, queries, "rpe-owner@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "rpe-other@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
	})

	created := createRPESession(t, app, ownerToken, map[string]interface{}{"rpe": 8})
	sessionID := created["id"].(string)

	resp, _ := updateRPESession(t, app, otherToken, sessionID, map[string]interface{}{
		"name":     "Stolen",
		"notes":    "",
		"duration": 3600,
		"rpe":      5,
	})

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected status %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

// The list endpoint is what the app history and the coach's session list both
// read, so the RPE has to survive the query that leaves the prescription out.
func TestSessionHandler_GetSessions_CarriesRPE(t *testing.T) {
	app, token := rpeApp(t, "rpe-list@test.com")

	createRPESession(t, app, token, map[string]interface{}{"rpe": 8})
	createRPESession(t, app, token, map[string]interface{}{"rpe_failed": true})

	req := testutil.NewJSONRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var sessions []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sessions)
	if len(sessions) != 2 {
		t.Fatalf("Expected 2 sessions, got %d", len(sessions))
	}

	var rated, failed int
	for _, session := range sessions {
		if session["rpe"] == float64(8) {
			rated++
		}
		if session["rpe_failed"] == true {
			failed++
		}
	}
	if rated != 1 {
		t.Errorf("Expected exactly one session carrying rpe 8, got %d", rated)
	}
	if failed != 1 {
		t.Errorf("Expected exactly one failed session, got %d", failed)
	}
}
