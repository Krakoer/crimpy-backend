package handler_test

import (
	"context"
	"crimpy/backend/internal/handler"
	"crimpy/backend/internal/utils"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestAuthHandler_Register_Success(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	// Prepare request
	reqBody := map[string]interface{}{
		"email":     "newuser@test.com",
		"password":  "password123",
		"firstname": "John",
		"lastname":  "Doe",
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/auth/register", body)

	// Execute request
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	// Assert response
	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["message"] != "User registered successfully" {
		t.Errorf("Expected success message, got %v", response["message"])
	}

	user, ok := response["user"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected user object in response")
	}

	if user["email"] != "newuser@test.com" {
		t.Errorf("Expected email 'newuser@test.com', got %v", user["email"])
	}
}

func TestAuthHandler_Register_DuplicateEmail(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	// Create first user
	reqBody := map[string]interface{}{
		"email":     "duplicate@test.com",
		"password":  "password123",
		"firstname": "John",
		"lastname":  "Doe",
	}
	body, _ := json.Marshal(reqBody)

	req1 := testutil.NewJSONRequest(http.MethodPost, "/auth/register", body)
	_, err := app.Test(req1)
	if err != nil {
		t.Fatalf("Failed to create first user: %v", err)
	}

	// Try to create second user with same email
	req2 := testutil.NewJSONRequest(http.MethodPost, "/auth/register", body)
	resp, err := app.Test(req2)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	// Assert response
	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("Expected status %d, got %d", fiber.StatusConflict, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["error"] != "Email already registered" {
		t.Errorf("Expected 'Email already registered' error, got %v", response["error"])
	}
}

func TestAuthHandler_Register_MissingFields(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	testCases := []struct {
		name     string
		reqBody  map[string]interface{}
		expected string
	}{
		{
			name: "missing email",
			reqBody: map[string]interface{}{
				"password":  "password123",
				"firstname": "John",
				"lastname":  "Doe",
			},
			expected: "All fields are required",
		},
		{
			name: "missing password",
			reqBody: map[string]interface{}{
				"email":     "test@test.com",
				"firstname": "John",
				"lastname":  "Doe",
			},
			expected: "All fields are required",
		},
		{
			name: "invalid email format",
			reqBody: map[string]interface{}{
				"email":     "invalidemail",
				"password":  "password123",
				"firstname": "John",
				"lastname":  "Doe",
			},
			expected: "Invalid email format",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.reqBody)
			req := testutil.NewJSONRequest(http.MethodPost, "/auth/register", body)

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Failed to execute request: %v", err)
			}

			if resp.StatusCode != fiber.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
			}

			var response map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&response)

			if response["error"] != tc.expected {
				t.Errorf("Expected error '%s', got %v", tc.expected, response["error"])
			}
		})
	}
}

func TestAuthHandler_Login_Success(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	// Register a user first
	email := "login@test.com"
	password := "password123"
	registerBody := map[string]interface{}{
		"email":     email,
		"password":  password,
		"firstname": "John",
		"lastname":  "Doe",
	}
	body, _ := json.Marshal(registerBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/auth/register", body)
	_, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to register user: %v", err)
	}

	// Now login
	loginBody := map[string]interface{}{
		"email":    email,
		"password": password,
	}
	body, _ = json.Marshal(loginBody)
	req = testutil.NewJSONRequest(http.MethodPost, "/auth/login", body)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute login request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["message"] != "Login successful" {
		t.Errorf("Expected 'Login successful', got %v", response["message"])
	}

	if response["token"] == nil || response["token"] == "" {
		t.Error("Expected token in response")
	}

	user, ok := response["user"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected user object in response")
	}

	if user["email"] != email {
		t.Errorf("Expected email '%s', got %v", email, user["email"])
	}
}

func TestAuthHandler_Login_InvalidCredentials(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	// Register a user first
	email := "login2@test.com"
	password := "password123"
	registerBody := map[string]interface{}{
		"email":     email,
		"password":  password,
		"firstname": "John",
		"lastname":  "Doe",
	}
	body, _ := json.Marshal(registerBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/auth/register", body)
	_, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to register user: %v", err)
	}

	testCases := []struct {
		name     string
		email    string
		password string
	}{
		{
			name:     "wrong password",
			email:    email,
			password: "wrongpassword",
		},
		{
			name:     "non-existent user",
			email:    "nonexistent@test.com",
			password: "password123",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			loginBody := map[string]interface{}{
				"email":    tc.email,
				"password": tc.password,
			}
			body, _ := json.Marshal(loginBody)
			req := testutil.NewJSONRequest(http.MethodPost, "/auth/login", body)

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Failed to execute login request: %v", err)
			}

			if resp.StatusCode != fiber.StatusUnauthorized {
				t.Errorf("Expected status %d, got %d", fiber.StatusUnauthorized, resp.StatusCode)
			}

			var response map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&response)

			if response["error"] != "Invalid credentials" {
				t.Errorf("Expected 'Invalid credentials', got %v", response["error"])
			}
		})
	}
}

func TestAuthHandler_Login_MissingFields(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	testCases := []struct {
		name    string
		reqBody map[string]interface{}
	}{
		{
			name: "missing email",
			reqBody: map[string]interface{}{
				"password": "password123",
			},
		},
		{
			name: "missing password",
			reqBody: map[string]interface{}{
				"email": "test@test.com",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.reqBody)
			req := testutil.NewJSONRequest(http.MethodPost, "/auth/login", body)

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Failed to execute request: %v", err)
			}

			if resp.StatusCode != fiber.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
			}

			var response map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&response)

			if response["error"] != "Email and password are required" {
				t.Errorf("Expected 'Email and password are required', got %v", response["error"])
			}
		})
	}
}

func TestAuthHandler_ChangePassword_Success(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	userID, token := testutil.CreateTestUser(t, queries, "changepass@test.com")

	reqBody := map[string]interface{}{
		"old_password": "password123",
		"new_password": "newpassword456",
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPut, "/api/auth/change-password", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["message"] != "Password updated successfully" {
		t.Errorf("Expected success message, got %v", response["message"])
	}

	_ = userID
}

func TestAuthHandler_ChangePassword_WrongOldPassword(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	_, token := testutil.CreateTestUser(t, queries, "wrongpass@test.com")

	reqBody := map[string]interface{}{
		"old_password": "wrongpassword",
		"new_password": "newpassword456",
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPut, "/api/auth/change-password", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", fiber.StatusUnauthorized, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["error"] != "Invalid old password" {
		t.Errorf("Expected 'Invalid old password', got %v", response["error"])
	}
}

func TestAuthHandler_ChangePassword_MissingOldPassword(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	_, token := testutil.CreateTestUser(t, queries, "missingold@test.com")

	reqBody := map[string]interface{}{
		"new_password": "newpassword456",
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPut, "/api/auth/change-password", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["error"] != "Old password is required" {
		t.Errorf("Expected 'Old password is required', got %v", response["error"])
	}
}

func TestAuthHandler_ChangePassword_ShortPassword(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	_, token := testutil.CreateTestUser(t, queries, "shortpass@test.com")

	reqBody := map[string]interface{}{
		"old_password": "password123",
		"new_password": "short",
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPut, "/api/auth/change-password", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["error"] != "New password must be at least 6 characters" {
		t.Errorf("Expected password length error, got %v", response["error"])
	}
}

func TestAuthHandler_ChangePassword_AdminChangesOtherUserPassword(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	_, adminToken := testutil.CreateTestAdminUser(t, queries, "admin@test.com")
	targetUserID, _ := testutil.CreateTestUser(t, queries, "targetuser@test.com")

	reqBody := map[string]interface{}{
		"user_id":      targetUserID,
		"new_password": "adminsetnewpass",
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPut, "/api/auth/change-password", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(adminToken))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["message"] != "Password updated successfully" {
		t.Errorf("Expected success message, got %v", response["message"])
	}
}

func TestAuthHandler_ChangePassword_NonAdminCannotChangeOtherUserPassword(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	_, userToken := testutil.CreateTestUser(t, queries, "normaluser@test.com")
	targetUserID, _ := testutil.CreateTestUser(t, queries, "targetuser2@test.com")

	reqBody := map[string]interface{}{
		"user_id":      targetUserID,
		"new_password": "trytochangeother",
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPut, "/api/auth/change-password", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(userToken))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected status %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["error"] != "Only admins can change other users' passwords" {
		t.Errorf("Expected admin only error, got %v", response["error"])
	}
}

func TestAuthHandler_ChangePassword_NoAuth(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	reqBody := map[string]interface{}{
		"old_password": "password123",
		"new_password": "newpassword456",
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPut, "/api/auth/change-password", body)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", fiber.StatusUnauthorized, resp.StatusCode)
	}
}

func TestAuthHandler_GetCurrentUser_Success(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	userID, token := testutil.CreateTestUser(t, queries, "getuser@test.com")

	req := testutil.NewJSONRequest(http.MethodGet, "/api/user", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var user map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&user)

	if user["id"] != userID {
		t.Errorf("Expected user ID %s, got %v", userID, user["id"])
	}

	if user["email"] != "getuser@test.com" {
		t.Errorf("Expected email getuser@test.com, got %v", user["email"])
	}

	if user["is_admin"] != false {
		t.Errorf("Expected is_admin false, got %v", user["is_admin"])
	}
}

func TestAuthHandler_GetCurrentUser_AdminUser(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	userID, token := testutil.CreateTestAdminUser(t, queries, "adminuser@test.com")

	req := testutil.NewJSONRequest(http.MethodGet, "/api/user", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var user map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&user)

	if user["id"] != userID {
		t.Errorf("Expected user ID %s, got %v", userID, user["id"])
	}

	if user["is_admin"] != true {
		t.Errorf("Expected is_admin true, got %v", user["is_admin"])
	}
}

func TestAuthHandler_GetCurrentUser_NoAuth(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	req := testutil.NewJSONRequest(http.MethodGet, "/api/user", nil)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", fiber.StatusUnauthorized, resp.StatusCode)
	}
}

func registerAndLogin(t *testing.T, app *fiber.App, email, password string) (string, string) {
	t.Helper()
	registerBody, _ := json.Marshal(map[string]interface{}{
		"email":     email,
		"password":  password,
		"firstname": "Refresh",
		"lastname":  "Tester",
	})
	if _, err := app.Test(testutil.NewJSONRequest(http.MethodPost, "/auth/register", registerBody)); err != nil {
		t.Fatalf("Failed to register user: %v", err)
	}

	loginBody, _ := json.Marshal(map[string]interface{}{"email": email, "password": password})
	resp, err := app.Test(testutil.NewJSONRequest(http.MethodPost, "/auth/login", loginBody))
	if err != nil {
		t.Fatalf("Failed to login: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 on login, got %d", resp.StatusCode)
	}
	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)
	token, _ := response["token"].(string)
	refreshToken, _ := response["refresh_token"].(string)
	return token, refreshToken
}

func TestAuthHandler_Login_ReturnsRefreshToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})

	_, refreshToken := registerAndLogin(t, app, "refresh-login@test.com", "password123")
	if refreshToken == "" {
		t.Error("Expected refresh_token in login response")
	}
}

func TestAuthHandler_Refresh_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})

	_, refreshToken := registerAndLogin(t, app, "refresh-ok@test.com", "password123")

	body, _ := json.Marshal(map[string]interface{}{"refresh_token": refreshToken})
	resp, err := app.Test(testutil.NewJSONRequest(http.MethodPost, "/auth/refresh", body))
	if err != nil {
		t.Fatalf("Refresh request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)
	if response["token"] == nil || response["token"] == "" {
		t.Error("Expected new access token")
	}
	newRefresh, _ := response["refresh_token"].(string)
	if newRefresh == "" || newRefresh == refreshToken {
		t.Error("Expected a rotated refresh token")
	}
}

func TestAuthHandler_Refresh_InvalidToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"refresh_token": "not-a-real-token"})
	resp, err := app.Test(testutil.NewJSONRequest(http.MethodPost, "/auth/refresh", body))
	if err != nil {
		t.Fatalf("Refresh request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}
}

func postRefresh(t *testing.T, app *fiber.App, refreshToken string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"refresh_token": refreshToken})
	resp, err := app.Test(testutil.NewJSONRequest(http.MethodPost, "/auth/refresh", body))
	if err != nil {
		t.Fatalf("Refresh request failed: %v", err)
	}
	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)
	rotated, _ := response["refresh_token"].(string)
	return resp.StatusCode, rotated
}

// A client whose rotation answer was lost still holds the token it sent. It
// must be able to present it again rather than be signed out.
func TestAuthHandler_Refresh_RotatedTokenReissuedWithinGrace(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})

	_, original := registerAndLogin(t, app, "refresh-grace@test.com", "password123")

	status, lost := postRefresh(t, app, original)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 on first refresh, got %d", status)
	}

	status, reissued := postRefresh(t, app, original)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 presenting the rotated token within the grace, got %d", status)
	}
	if reissued == "" || reissued == lost || reissued == original {
		t.Fatal("Expected a fresh successor on the reissue")
	}

	if status, _ := postRefresh(t, app, lost); status != fiber.StatusUnauthorized {
		t.Errorf("Expected the undelivered successor to be revoked, got %d", status)
	}
	if status, _ := postRefresh(t, app, reissued); status != fiber.StatusOK {
		t.Errorf("Expected the reissued successor to work, got %d", status)
	}
}

// A successor that was presented proves the client got it, so the token it
// replaced is spent for good.
func TestAuthHandler_Refresh_RotatedTokenRejectedOnceSuccessorUsed(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})

	_, original := registerAndLogin(t, app, "refresh-rotate@test.com", "password123")

	_, successor := postRefresh(t, app, original)
	if status, _ := postRefresh(t, app, successor); status != fiber.StatusOK {
		t.Fatalf("Expected 200 refreshing with the successor, got %d", status)
	}

	if status, _ := postRefresh(t, app, original); status != fiber.StatusUnauthorized {
		t.Errorf("Expected 401 reusing a token whose successor was used, got %d", status)
	}
}

func TestAuthHandler_Refresh_RotatedTokenRejectedAfterGrace(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})

	_, original := registerAndLogin(t, app, "refresh-grace-over@test.com", "password123")
	postRefresh(t, app, original)

	_, err := pool.Exec(context.Background(),
		"UPDATE refresh_tokens SET revoked_at = now() - $1::interval WHERE token_hash = $2",
		fmt.Sprintf("%d seconds", int(utils.RefreshTokenReuseGrace.Seconds())+1), utils.HashToken(original))
	if err != nil {
		t.Fatalf("Failed to age the rotation: %v", err)
	}

	if status, _ := postRefresh(t, app, original); status != fiber.StatusUnauthorized {
		t.Errorf("Expected 401 once the grace is over, got %d", status)
	}
}

// Signing out ends the chain on purpose, and the grace must not reopen it.
func TestAuthHandler_Refresh_RotatedTokenRejectedAfterLogout(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})

	_, original := registerAndLogin(t, app, "refresh-logout@test.com", "password123")
	_, successor := postRefresh(t, app, original)

	body, _ := json.Marshal(map[string]interface{}{"refresh_token": successor})
	if _, err := app.Test(testutil.NewJSONRequest(http.MethodPost, "/auth/logout", body)); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	if status, _ := postRefresh(t, app, original); status != fiber.StatusUnauthorized {
		t.Errorf("Expected 401 reusing a rotated token after logout, got %d", status)
	}
}

// A password change revokes every token of the account, including a rotated
// one still inside its grace.
func TestAuthHandler_Refresh_RotatedTokenRejectedAfterRevokeAll(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})

	_, original := registerAndLogin(t, app, "refresh-revoke-all@test.com", "password123")
	postRefresh(t, app, original)

	stored, err := queries.GetRefreshTokenByHash(context.Background(), utils.HashToken(original))
	if err != nil {
		t.Fatalf("Failed to load the rotated token: %v", err)
	}
	if err := queries.RevokeUserRefreshTokens(context.Background(), stored.UserID); err != nil {
		t.Fatalf("Failed to revoke the account's tokens: %v", err)
	}

	if status, _ := postRefresh(t, app, original); status != fiber.StatusUnauthorized {
		t.Errorf("Expected 401 reusing a rotated token after revoking all, got %d", status)
	}
}

// A client signing out with a rotated token never received its successor, and
// that successor must not outlive the sign out.
func TestAuthHandler_Logout_WithRotatedTokenRevokesUndeliveredSuccessor(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})

	_, original := registerAndLogin(t, app, "logout-rotated@test.com", "password123")
	_, undelivered := postRefresh(t, app, original)

	body, _ := json.Marshal(map[string]interface{}{"refresh_token": original})
	if _, err := app.Test(testutil.NewJSONRequest(http.MethodPost, "/auth/logout", body)); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	if status, _ := postRefresh(t, app, undelivered); status != fiber.StatusUnauthorized {
		t.Errorf("Expected the undelivered successor to be revoked by the logout, got %d", status)
	}
	if status, _ := postRefresh(t, app, original); status != fiber.StatusUnauthorized {
		t.Errorf("Expected the logged out token to stay revoked, got %d", status)
	}
}
