package handler_test

import (
	"bytes"
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

	req, _ := http.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	req1, _ := http.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	_, err := app.Test(req1)
	if err != nil {
		t.Fatalf("Failed to create first user: %v", err)
	}

	// Try to create second user with same email
	req2, _ := http.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
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
			req, _ := http.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

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
	req, _ := http.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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
	req, _ = http.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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
	req, _ := http.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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
			req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

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
			req, _ := http.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

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
