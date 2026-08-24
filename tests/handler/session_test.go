package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestSessionHandler_CreateSession_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	userID, token := testutil.CreateTestUser(t, queries, "session1@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"name":          "Test Session",
		"notes":         "Some notes",
		"is_assessment": false,
		"activity":      1,
		"duration":      3600,
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	if session["name"] != "Test Session" {
		t.Errorf("Expected name 'Test Session', got %v", session["name"])
	}

	if session["user_id"] != userID {
		t.Errorf("Expected user_id '%s', got %v", userID, session["user_id"])
	}
}

func TestSessionHandler_CreateSession_MissingName(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session2@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"activity": 1,
		"duration": 3600,
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

func TestSessionHandler_CreateSession_Unauthorized(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"name":     "Test Session",
		"activity": 1,
		"duration": 3600,
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	// No authorization header

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", fiber.StatusUnauthorized, resp.StatusCode)
	}
}

func TestSessionHandler_GetSessions_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session3@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// Create a session first
	reqBody := map[string]interface{}{
		"name":     "Test Session",
		"activity": 1,
		"duration": 3600,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	_, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Get sessions
	req = testutil.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var sessions []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sessions)

	if len(sessions) == 0 {
		t.Fatal("Expected at least one session")
	}

	// The app reads the rep count from the list rather than fetching every
	// session's reps, so the key has to be there even when nothing was recorded.
	if _, ok := sessions[0]["rep_count"]; !ok {
		t.Errorf("Expected a rep_count on the listed session, got %v", sessions[0])
	}
	if sessions[0]["origin"] == nil {
		t.Errorf("Expected an origin on the listed session, got %v", sessions[0])
	}
}

func TestSessionHandler_GetSession_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session4@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// Create a session
	reqBody := map[string]interface{}{
		"name":     "Test Session",
		"notes":    "Test notes",
		"activity": 1,
		"duration": 3600,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	var createdSession map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdSession)
	sessionID := createdSession["id"].(string)

	// Get the session
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	session, ok := response["session"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected session object in response")
	}

	if session["name"] != "Test Session" {
		t.Errorf("Expected name 'Test Session', got %v", session["name"])
	}
}

func TestSessionHandler_GetSession_UserIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	// Create two users
	_, token1 := testutil.CreateTestUser(t, queries, "session5@test.com")
	_, token2 := testutil.CreateTestUser(t, queries, "session6@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// User 1 creates a session
	reqBody := map[string]interface{}{
		"name":     "User 1 Session",
		"activity": 1,
		"duration": 3600,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	var createdSession map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdSession)
	sessionID := createdSession["id"].(string)

	// User 2 tries to access User 1's session
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	// Should be forbidden
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected status %d (Forbidden), got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestSessionHandler_UpdateSession_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session7@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// Create a session
	reqBody := map[string]interface{}{
		"name":     "Original Session",
		"notes":    "Original notes",
		"activity": 1,
		"duration": 3600,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	var createdSession map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdSession)
	sessionID := createdSession["id"].(string)

	// Update the session
	updateBody := map[string]interface{}{
		"name":     "Updated Session",
		"notes":    "Updated notes",
		"duration": 7200,
	}
	body, _ = json.Marshal(updateBody)
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/sessions/%s", sessionID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var updatedSession map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updatedSession)

	if updatedSession["name"] != "Updated Session" {
		t.Errorf("Expected name 'Updated Session', got %v", updatedSession["name"])
	}

	if updatedSession["notes"] != "Updated notes" {
		t.Errorf("Expected notes 'Updated notes', got %v", updatedSession["notes"])
	}
}

func TestSessionHandler_UpdateSession_UserIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	// Create two users
	_, token1 := testutil.CreateTestUser(t, queries, "session8@test.com")
	_, token2 := testutil.CreateTestUser(t, queries, "session9@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// User 1 creates a session
	reqBody := map[string]interface{}{
		"name":     "User 1 Session",
		"activity": 1,
		"duration": 3600,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	var createdSession map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdSession)
	sessionID := createdSession["id"].(string)

	// User 2 tries to update User 1's session
	updateBody := map[string]interface{}{
		"name":     "Hacked Session",
		"notes":    "Hacked",
		"duration": 1,
	}
	body, _ = json.Marshal(updateBody)
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/sessions/%s", sessionID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	// Should be forbidden
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected status %d (Forbidden), got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestSessionHandler_DeleteSession_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session10@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// Create a session
	reqBody := map[string]interface{}{
		"name":     "To Delete",
		"activity": 1,
		"duration": 3600,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	var createdSession map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdSession)
	sessionID := createdSession["id"].(string)

	// Delete the session
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	// Try to get the deleted session
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected status %d after deletion, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}

func TestSessionHandler_DeleteSession_UserIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	// Create two users
	_, token1 := testutil.CreateTestUser(t, queries, "session11@test.com")
	_, token2 := testutil.CreateTestUser(t, queries, "session12@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// User 1 creates a session
	reqBody := map[string]interface{}{
		"name":     "User 1 Session",
		"activity": 1,
		"duration": 3600,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	var createdSession map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdSession)
	sessionID := createdSession["id"].(string)

	// User 2 tries to delete User 1's session
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	// Should be forbidden
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected status %d (Forbidden), got %d", fiber.StatusForbidden, resp.StatusCode)
	}

	// Verify session still exists for user 1
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusOK {
		t.Error("Session should still exist for original owner")
	}
}

func TestSessionHandler_CreateSession_RepDataEdgeSize(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session9@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"name":     "Edge Session",
		"notes":    "",
		"activity": 0,
		"duration": 60,
		"rep_datas": []map[string]interface{}{
			{
				"average_weight": 30.0,
				"is_rest":        false,
				"right_hand":     true,
				"duration":       7,
				"target_weight":  35.0,
				"index":          0,
				"grip_position":  0,
				"edge_size_mm":   20,
			},
			{
				"average_weight": 0.0,
				"is_rest":        true,
				"right_hand":     true,
				"duration":       3,
				"target_weight":  0.0,
				"index":          1,
				"grip_position":  0,
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var createdSession map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdSession)
	sessionID := createdSession["id"].(string)

	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	repDatas, ok := response["rep_datas"].([]interface{})
	if !ok || len(repDatas) != 2 {
		t.Fatalf("Expected 2 rep datas, got %v", response["rep_datas"])
	}

	work := repDatas[0].(map[string]interface{})
	if work["edge_size_mm"] != float64(20) {
		t.Errorf("Expected edge size 20 on the work rep, got %v", work["edge_size_mm"])
	}

	rest := repDatas[1].(map[string]interface{})
	if rest["edge_size_mm"] != nil {
		t.Errorf("Expected no edge size on the rest rep, got %v", rest["edge_size_mm"])
	}
}

// assertSessionDate compares instants rather than text: the API renders the
// stored timestamptz in the server zone, so the same moment comes back with a
// different offset than the UTC string that was sent.
func assertSessionDate(t *testing.T, session map[string]interface{}, wantRFC3339 string) {
	t.Helper()

	want, err := time.Parse(time.RFC3339, wantRFC3339)
	if err != nil {
		t.Fatalf("Bad expected date %q: %v", wantRFC3339, err)
	}

	raw, _ := session["date"].(string)
	got, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("Could not parse returned date %q: %v", raw, err)
	}

	if !got.Equal(want) {
		t.Errorf("Expected date %s, got %s", want.UTC(), got.UTC())
	}
}

func TestSessionHandler_CreateSession_DefaultsToLoggedOrigin(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session-origin-default@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"name":     "Evening run",
		"notes":    "",
		"activity": 4,
		"duration": 1800,
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	if session["origin"] != "logged" {
		t.Errorf("Expected origin 'logged', got %v", session["origin"])
	}
	if session["activity"] != float64(4) {
		t.Errorf("Expected activity 4, got %v", session["activity"])
	}
}

func TestSessionHandler_CreateSession_PlayedOriginIsStored(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session-origin-played@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"name":     "Coach hangboard block",
		"notes":    "",
		"activity": 0,
		"origin":   "played",
		"duration": 600,
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	if session["origin"] != "played" {
		t.Errorf("Expected origin 'played', got %v", session["origin"])
	}
}

func TestSessionHandler_CreateSession_RejectsUnknownOrigin(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session-origin-bad@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"name":     "Bogus",
		"notes":    "",
		"activity": 0,
		"origin":   "imported",
		"duration": 60,
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

func TestSessionHandler_CreateSession_RejectsUnknownActivity(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session-activity-bad@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	for _, activity := range []int{-1, 5} {
		reqBody := map[string]interface{}{
			"name":     "Bogus",
			"notes":    "",
			"activity": activity,
			"duration": 60,
		}
		body, _ := json.Marshal(reqBody)

		req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
		req.Header.Set("Authorization", testutil.GetAuthHeader(token))

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to execute request: %v", err)
		}

		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Expected status %d for activity %d, got %d", fiber.StatusBadRequest, activity, resp.StatusCode)
		}
	}
}

// postSessionWithLinks creates a session carrying the optional template links
// and returns the status the API answered with.
func postSessionWithLinks(t *testing.T, app *fiber.App, token string, links map[string]interface{}) int {
	t.Helper()

	reqBody := map[string]interface{}{
		"name":     "Prescribed hangboard",
		"notes":    "",
		"activity": 0,
		"origin":   "played",
		"duration": 600,
	}
	for key, value := range links {
		reqBody[key] = value
	}
	body, _ := json.Marshal(reqBody)

	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, token))
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	return resp.StatusCode
}

func TestSessionHandler_CreateSession_AcceptsOwnTraining(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session-link-own@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:  handler.NewSessionHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	trainingID := createTestCoachTraining(t, token, app)

	status := postSessionWithLinks(t, app, token, map[string]interface{}{"training_id": trainingID})
	if status != fiber.StatusCreated {
		t.Errorf("Expected status %d for an own training, got %d", fiber.StatusCreated, status)
	}
}

func TestSessionHandler_CreateSession_AcceptsProgramTraining(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "session-link-coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "session-link-athlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:  handler.NewSessionHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	weekBody, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), weekBody, coachToken))
	if err != nil {
		t.Fatalf("Failed to upsert week: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 upserting week, got %d", resp.StatusCode)
	}

	var week map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&week)
	weekSessions := week["sessions"].([]interface{})
	programSessionID := weekSessions[0].(map[string]interface{})["id"].(string)

	status := postSessionWithLinks(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})
	if status != fiber.StatusCreated {
		t.Errorf("Expected status %d for a prescribed training, got %d", fiber.StatusCreated, status)
	}
}

func TestSessionHandler_CreateSession_RejectsForeignAndUnknownLinks(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "session-foreign-coach@test.com")
	strangerID, _ := testutil.CreateTestUser(t, queries, "session-foreign-stranger@test.com")
	_, token := testutil.CreateTestUser(t, queries, "session-foreign-athlete@test.com")
	enrollUserDirect(t, pool, coachID, strangerID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:  handler.NewSessionHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, strangerID, app)
	foreignTrainingID := createTestCoachTraining(t, coachToken, app)

	weekBody, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": foreignTrainingID, "day_of_week": 0}},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(strangerID, programID, 1), weekBody, coachToken))
	if err != nil {
		t.Fatalf("Failed to upsert week: %v", err)
	}
	var week map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&week)
	weekSessions := week["sessions"].([]interface{})
	foreignProgramSessionID := weekSessions[0].(map[string]interface{})["id"].(string)

	unknownID := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name  string
		links map[string]interface{}
	}{
		{"foreign training", map[string]interface{}{"training_id": foreignTrainingID}},
		{"unknown training", map[string]interface{}{"training_id": unknownID}},
		{"foreign program session", map[string]interface{}{"program_session_id": foreignProgramSessionID}},
		{"unknown program session", map[string]interface{}{"program_session_id": unknownID}},
	}

	for _, tc := range cases {
		status := postSessionWithLinks(t, app, token, tc.links)
		if status != fiber.StatusForbidden {
			t.Errorf("Expected status %d for a %s, got %d", fiber.StatusForbidden, tc.name, status)
		}
	}
}

func TestSessionHandler_UpdateSession_ChangesDate(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session-update-date@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"name":     "Logged climbing",
		"notes":    "",
		"activity": 1,
		"date":     "2026-08-01T10:00:00Z",
		"duration": 5400,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	sessionID := created["id"].(string)

	updateBody := map[string]interface{}{
		"name":     "Logged climbing",
		"notes":    "Moved to the right evening",
		"duration": 5400,
		"date":     "2026-08-03T18:30:00Z",
	}
	body, _ = json.Marshal(updateBody)
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/sessions/%s", sessionID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var updated map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updated)

	assertSessionDate(t, updated, "2026-08-03T18:30:00Z")
}

func TestSessionHandler_UpdateSession_KeepsDateWhenOmitted(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session-keep-date@test.com")

	sessionHandler := handler.NewSessionHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"name":     "Played hangboard",
		"notes":    "",
		"activity": 0,
		"origin":   "played",
		"date":     "2026-08-01T10:00:00Z",
		"duration": 600,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	sessionID := created["id"].(string)

	updateBody := map[string]interface{}{
		"name":     "Played hangboard",
		"notes":    "Felt strong",
		"duration": 600,
	}
	body, _ = json.Marshal(updateBody)
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/sessions/%s", sessionID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	var updated map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updated)

	assertSessionDate(t, updated, "2026-08-01T10:00:00Z")
}

// createTestTwoBlockTraining makes the training the reps-per-item link exists
// for: two hangboard blocks on different edges, the case a flat rep list cannot
// be read against its prescription.
func createTestTwoBlockTraining(t *testing.T, token string, app *fiber.App) (trainingID string, itemIDs []string) {
	t.Helper()
	block := func(edge int) map[string]interface{} {
		return map[string]interface{}{
			"type":             "hangboard_rep",
			"worktime_seconds": 7,
			"rest_seconds":     60,
			"hand":             "both",
			"granularity":      "uniform",
			"loads":            []map[string]interface{}{{"value": 0, "unit": "bw"}},
			"edge_sizes_mm":    []int{edge},
		}
	}
	body, _ := json.Marshal(map[string]interface{}{
		"title":         "Two Block Hangboard",
		"training_type": "hangboard",
		"items":         []map[string]interface{}{block(20), block(14)},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/trainings", body, token))
	if err != nil {
		t.Fatalf("Failed to create training: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating training, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	trainingID = result["id"].(string)
	for _, raw := range result["items"].([]interface{}) {
		itemIDs = append(itemIDs, raw.(map[string]interface{})["id"].(string))
	}
	if len(itemIDs) != 2 {
		t.Fatalf("Expected 2 items on the training, got %d", len(itemIDs))
	}
	return trainingID, itemIDs
}

func TestSessionHandler_CreateSession_LinksRepsToPrescriptionItems(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session-rep-item@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:  handler.NewSessionHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	trainingID, itemIDs := createTestTwoBlockTraining(t, token, app)

	reqBody := map[string]interface{}{
		"name":        "Two Block Session",
		"notes":       "",
		"activity":    0,
		"origin":      "played",
		"duration":    600,
		"training_id": trainingID,
		"rep_datas": []map[string]interface{}{
			{
				"average_weight":   30.0,
				"is_rest":          false,
				"right_hand":       true,
				"duration":         7,
				"target_weight":    35.0,
				"index":            0,
				"grip_position":    0,
				"edge_size_mm":     20,
				"training_item_id": itemIDs[0],
			},
			{
				"average_weight":   22.0,
				"is_rest":          false,
				"right_hand":       true,
				"duration":         7,
				"target_weight":    25.0,
				"index":            1,
				"grip_position":    0,
				"edge_size_mm":     14,
				"training_item_id": itemIDs[1],
			},
			{
				"average_weight": 0.0,
				"is_rest":        true,
				"right_hand":     true,
				"duration":       3,
				"target_weight":  0.0,
				"index":          2,
				"grip_position":  0,
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, token))
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}
	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)

	req := testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%s", created["id"].(string)), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to read session: %v", err)
	}
	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	repDatas, ok := response["rep_datas"].([]interface{})
	if !ok || len(repDatas) != 3 {
		t.Fatalf("Expected 3 rep datas, got %v", response["rep_datas"])
	}

	for i, wanted := range []interface{}{itemIDs[0], itemIDs[1], nil} {
		got := repDatas[i].(map[string]interface{})["training_item_id"]
		if got != wanted {
			t.Errorf("Expected rep %d linked to %v, got %v", i, wanted, got)
		}
	}
}

func TestSessionHandler_CreateSession_DropsRepItemOutsidePrescription(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session-rep-item-bad@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "session-rep-item-other@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:  handler.NewSessionHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	trainingID, _ := createTestTwoBlockTraining(t, token, app)
	_, otherItemIDs := createTestTwoBlockTraining(t, token, app)
	_, foreignItemIDs := createTestTwoBlockTraining(t, otherToken, app)

	// A link the frozen prescription does not hold is a coach edit landing
	// mid-run, not a client bug: the rep is kept and only the grouping is lost.
	dropped := []struct {
		name   string
		links  map[string]interface{}
		itemID string
	}{
		{
			name:   "item of another training",
			links:  map[string]interface{}{"training_id": trainingID},
			itemID: otherItemIDs[0],
		},
		{
			name:   "item of another user's training",
			links:  map[string]interface{}{"training_id": trainingID},
			itemID: foreignItemIDs[0],
		},
		{
			name:   "session with no prescription",
			links:  map[string]interface{}{},
			itemID: otherItemIDs[0],
		},
		{
			name:   "unknown id",
			links:  map[string]interface{}{"training_id": trainingID},
			itemID: "00000000-0000-0000-0000-000000000000",
		},
	}

	for _, tc := range dropped {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := app.Test(testutil.NewJSONRequestWithAuth(
				http.MethodPost, "/api/sessions", repItemLinkPayload(tc.links, tc.itemID), token))
			if err != nil {
				t.Fatalf("Failed to create session: %v", err)
			}
			if resp.StatusCode != fiber.StatusCreated {
				t.Fatalf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
			}
			var created map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&created)

			req := testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%s", created["id"].(string)), nil)
			req.Header.Set("Authorization", testutil.GetAuthHeader(token))
			resp, err = app.Test(req)
			if err != nil {
				t.Fatalf("Failed to read session: %v", err)
			}
			var response map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&response)

			repDatas, ok := response["rep_datas"].([]interface{})
			if !ok || len(repDatas) != 1 {
				t.Fatalf("Expected 1 rep data, got %v", response["rep_datas"])
			}
			if got := repDatas[0].(map[string]interface{})["training_item_id"]; got != nil {
				t.Errorf("Expected the unprescribed link dropped to null, got %v", got)
			}
			if got := repDatas[0].(map[string]interface{})["average_weight"]; got != 30.0 {
				t.Errorf("Expected the rep itself kept, got average_weight %v", got)
			}
		})
	}

	// A malformed id cannot come from a race, so it still fails the request.
	t.Run("malformed id", func(t *testing.T) {
		resp, err := app.Test(testutil.NewJSONRequestWithAuth(
			http.MethodPost, "/api/sessions",
			repItemLinkPayload(map[string]interface{}{"training_id": trainingID}, "not-a-uuid"), token))
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
		}
		var body map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&body)
		if got, _ := body["error"].(string); !strings.Contains(got, "rep 0") {
			t.Errorf("Expected the error to name the offending rep, got %q", got)
		}
	})
}

// repItemLinkPayload is a one-rep session body carrying itemID as its link.
func repItemLinkPayload(links map[string]interface{}, itemID string) []byte {
	payload := map[string]interface{}{
		"name":     "Bad Link Session",
		"notes":    "",
		"activity": 0,
		"origin":   "played",
		"duration": 600,
		"rep_datas": []map[string]interface{}{
			{
				"average_weight":   30.0,
				"is_rest":          false,
				"right_hand":       true,
				"duration":         7,
				"target_weight":    35.0,
				"index":            0,
				"grip_position":    0,
				"training_item_id": itemID,
			},
		},
	}
	for k, v := range links {
		payload[k] = v
	}
	body, _ := json.Marshal(payload)
	return body
}

func TestSessionHandler_CreateSession_KeepsTargetUnmeasuredOnReps(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session-rep-unmeasured@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
	})

	reqBody := map[string]interface{}{
		"name":     "Dropped Sensor Session",
		"notes":    "",
		"activity": 0,
		"origin":   "played",
		"duration": 300,
		"rep_datas": []map[string]interface{}{
			{
				"average_weight": 30.0,
				"is_rest":        false,
				"right_hand":     true,
				"duration":       7,
				"target_weight":  32.0,
				"index":          0,
				"grip_position":  0,
			},
			{
				"average_weight":    0.0,
				"is_rest":           false,
				"right_hand":        true,
				"duration":          7,
				"target_weight":     0.0,
				"index":             1,
				"grip_position":     0,
				"target_unmeasured": true,
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, token))
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}
	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)

	req := testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%s", created["id"].(string)), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to read session: %v", err)
	}
	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	repDatas, ok := response["rep_datas"].([]interface{})
	if !ok || len(repDatas) != 2 {
		t.Fatalf("Expected 2 rep datas, got %v", response["rep_datas"])
	}
	// A measured rep sends nothing, so the flag has to read false rather than be
	// missing: the clients grade on it.
	for i, wanted := range []interface{}{false, true} {
		got := repDatas[i].(map[string]interface{})["target_unmeasured"]
		if got != wanted {
			t.Errorf("Expected rep %d target_unmeasured %v, got %v", i, wanted, got)
		}
	}
}
