package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
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

	if session["Name"] != "Test Session" {
		t.Errorf("Expected name 'Test Session', got %v", session["Name"])
	}

	if session["UserID"] != userID {
		t.Errorf("Expected user_id '%s', got %v", userID, session["UserID"])
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
		t.Error("Expected at least one session")
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
	sessionID := createdSession["ID"].(string)

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

	if session["Name"] != "Test Session" {
		t.Errorf("Expected name 'Test Session', got %v", session["Name"])
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
	sessionID := createdSession["ID"].(string)

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
	sessionID := createdSession["ID"].(string)

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

	if updatedSession["Name"] != "Updated Session" {
		t.Errorf("Expected name 'Updated Session', got %v", updatedSession["Name"])
	}

	if updatedSession["Notes"] != "Updated notes" {
		t.Errorf("Expected notes 'Updated notes', got %v", updatedSession["Notes"])
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
	sessionID := createdSession["ID"].(string)

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
	sessionID := createdSession["ID"].(string)

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
	sessionID := createdSession["ID"].(string)

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
	sessionID := createdSession["ID"].(string)

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
	if work["EdgeSizeMm"] != float64(20) {
		t.Errorf("Expected edge size 20 on the work rep, got %v", work["EdgeSizeMm"])
	}

	rest := repDatas[1].(map[string]interface{})
	if rest["EdgeSizeMm"] != nil {
		t.Errorf("Expected no edge size on the rest rep, got %v", rest["EdgeSizeMm"])
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

	raw, _ := session["Date"].(string)
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

	if session["Origin"] != "logged" {
		t.Errorf("Expected origin 'logged', got %v", session["Origin"])
	}
	if session["Activity"] != float64(4) {
		t.Errorf("Expected activity 4, got %v", session["Activity"])
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

	if session["Origin"] != "played" {
		t.Errorf("Expected origin 'played', got %v", session["Origin"])
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
	sessionID := created["ID"].(string)

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
	sessionID := created["ID"].(string)

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
