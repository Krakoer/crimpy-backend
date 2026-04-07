package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestSessionHandler_CreateSession_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	userID, token := testutil.CreateTestUser(t, queries, "session1@test.com")

	sessionHandler := handler.NewSessionHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"name":          "Test Session",
		"notes":         "Some notes",
		"is_assessment": false,
		"session_type":  1,
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
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session2@test.com")

	sessionHandler := handler.NewSessionHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"session_type": 1,
		"duration":     3600,
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

	sessionHandler := handler.NewSessionHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	reqBody := map[string]interface{}{
		"name":         "Test Session",
		"session_type": 1,
		"duration":     3600,
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
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session3@test.com")

	sessionHandler := handler.NewSessionHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// Create a session first
	reqBody := map[string]interface{}{
		"name":         "Test Session",
		"session_type": 1,
		"duration":     3600,
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
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session4@test.com")

	sessionHandler := handler.NewSessionHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// Create a session
	reqBody := map[string]interface{}{
		"name":         "Test Session",
		"notes":        "Test notes",
		"session_type": 1,
		"duration":     3600,
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
	sessionID := int(createdSession["ID"].(float64))

	// Get the session
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%d", sessionID), nil)
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
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	// Create two users
	_, token1 := testutil.CreateTestUser(t, queries, "session5@test.com")
	_, token2 := testutil.CreateTestUser(t, queries, "session6@test.com")

	sessionHandler := handler.NewSessionHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// User 1 creates a session
	reqBody := map[string]interface{}{
		"name":         "User 1 Session",
		"session_type": 1,
		"duration":     3600,
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
	sessionID := int(createdSession["ID"].(float64))

	// User 2 tries to access User 1's session
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%d", sessionID), nil)
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
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session7@test.com")

	sessionHandler := handler.NewSessionHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// Create a session
	reqBody := map[string]interface{}{
		"name":         "Original Session",
		"notes":        "Original notes",
		"session_type": 1,
		"duration":     3600,
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
	sessionID := int(createdSession["ID"].(float64))

	// Update the session
	updateBody := map[string]interface{}{
		"name":     "Updated Session",
		"notes":    "Updated notes",
		"duration": 7200,
	}
	body, _ = json.Marshal(updateBody)
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/sessions/%d", sessionID), body)
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
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	// Create two users
	_, token1 := testutil.CreateTestUser(t, queries, "session8@test.com")
	_, token2 := testutil.CreateTestUser(t, queries, "session9@test.com")

	sessionHandler := handler.NewSessionHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// User 1 creates a session
	reqBody := map[string]interface{}{
		"name":         "User 1 Session",
		"session_type": 1,
		"duration":     3600,
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
	sessionID := int(createdSession["ID"].(float64))

	// User 2 tries to update User 1's session
	updateBody := map[string]interface{}{
		"name":     "Hacked Session",
		"notes":    "Hacked",
		"duration": 1,
	}
	body, _ = json.Marshal(updateBody)
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/sessions/%d", sessionID), body)
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
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "session10@test.com")

	sessionHandler := handler.NewSessionHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// Create a session
	reqBody := map[string]interface{}{
		"name":         "To Delete",
		"session_type": 1,
		"duration":     3600,
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
	sessionID := int(createdSession["ID"].(float64))

	// Delete the session
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/sessions/%d", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	// Try to get the deleted session
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%d", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected status %d after deletion, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}

func TestSessionHandler_DeleteSession_UserIsolation(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	// Create two users
	_, token1 := testutil.CreateTestUser(t, queries, "session11@test.com")
	_, token2 := testutil.CreateTestUser(t, queries, "session12@test.com")

	sessionHandler := handler.NewSessionHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: sessionHandler,
	})

	// User 1 creates a session
	reqBody := map[string]interface{}{
		"name":         "User 1 Session",
		"session_type": 1,
		"duration":     3600,
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
	sessionID := int(createdSession["ID"].(float64))

	// User 2 tries to delete User 1's session
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/sessions/%d", sessionID), nil)
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
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/sessions/%d", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusOK {
		t.Error("Session should still exist for original owner")
	}
}
