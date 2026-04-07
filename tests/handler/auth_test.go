package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestAuthHandler_Register_Success(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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

	authHandler := handler.NewAuthHandler(queries)
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
