package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestAuthHandler_RegisterCoach_Success(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	reqBody := map[string]interface{}{
		"email":     "coach@test.com",
		"password":  "password123",
		"firstname": "Coach",
		"lastname":  "User",
		"is_coach":  true,
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/auth/register", body)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["message"] != "Coach account created. Pending admin validation." {
		t.Errorf("Expected coach creation message, got %v", response["message"])
	}

	user, ok := response["user"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected user object in response")
	}

	if user["email"] != "coach@test.com" {
		t.Errorf("Expected email 'coach@test.com', got %v", user["email"])
	}

	if user["is_coach"] != true {
		t.Errorf("Expected is_coach to be true, got %v", user["is_coach"])
	}

	if user["coach_validated"] != false {
		t.Errorf("Expected coach_validated to be false, got %v", user["coach_validated"])
	}
}

func TestAuthHandler_RegisterRegularUser_CoachFieldsFalse(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	reqBody := map[string]interface{}{
		"email":     "regular@test.com",
		"password":  "password123",
		"firstname": "Regular",
		"lastname":  "User",
		"is_coach":  false,
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/auth/register", body)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["message"] != "User registered successfully" {
		t.Errorf("Expected user registration message, got %v", response["message"])
	}

	user, ok := response["user"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected user object in response")
	}

	if user["is_coach"] != false {
		t.Errorf("Expected is_coach to be false, got %v", user["is_coach"])
	}

	if user["coach_validated"] != false {
		t.Errorf("Expected coach_validated to be false, got %v", user["coach_validated"])
	}
}

func TestAuthHandler_LoginCoach_ReturnsCoachFields(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	authHandler := handler.NewAuthHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: authHandler,
	})

	email := "coachlogin@test.com"
	password := "password123"

	registerBody := map[string]interface{}{
		"email":     email,
		"password":  password,
		"firstname": "Coach",
		"lastname":  "User",
		"is_coach":  true,
	}
	body, _ := json.Marshal(registerBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/auth/register", body)
	_, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to register coach: %v", err)
	}

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

	user, ok := response["user"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected user object in response")
	}

	if user["is_coach"] != true {
		t.Errorf("Expected is_coach to be true, got %v", user["is_coach"])
	}

	if user["coach_validated"] != false {
		t.Errorf("Expected coach_validated to be false for unvalidated coach, got %v", user["coach_validated"])
	}
}

func TestAdminHandler_GetPendingCoaches_Success(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	adminHandler := handler.NewAdminHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AdminHandler: adminHandler,
	})

	_, adminToken := testutil.CreateTestAdminUser(t, queries, "admin@test.com")
	testutil.CreateTestCoachUser(t, queries, "pendingcoach1@test.com")
	testutil.CreateTestCoachUser(t, queries, "pendingcoach2@test.com")
	testutil.CreateTestUser(t, queries, "regularuser@test.com")

	req := testutil.NewJSONRequest(http.MethodGet, "/api/admin/coaches/pending", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(adminToken))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var coaches []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&coaches)

	if len(coaches) != 2 {
		t.Errorf("Expected 2 pending coaches, got %d", len(coaches))
	}

	for _, coach := range coaches {
		if coach["is_coach"] != true {
			t.Errorf("Expected is_coach to be true, got %v", coach["is_coach"])
		}
		if coach["coach_validated"] != false {
			t.Errorf("Expected coach_validated to be false, got %v", coach["coach_validated"])
		}
	}
}

func TestAdminHandler_GetPendingCoaches_NonAdminForbidden(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	adminHandler := handler.NewAdminHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AdminHandler: adminHandler,
	})

	_, userToken := testutil.CreateTestUser(t, queries, "regularuser@test.com")

	req := testutil.NewJSONRequest(http.MethodGet, "/api/admin/coaches/pending", nil)
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

	if response["error"] != "Admin access required" {
		t.Errorf("Expected admin access error, got %v", response["error"])
	}
}

func TestAdminHandler_ValidateCoach_Success(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	adminHandler := handler.NewAdminHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AdminHandler: adminHandler,
	})

	_, adminToken := testutil.CreateTestAdminUser(t, queries, "admin@test.com")
	coachID, _ := testutil.CreateTestCoachUser(t, queries, "pendingcoach@test.com")

	req := testutil.NewJSONRequest(http.MethodPut, "/api/admin/coaches/"+coachID+"/validate", nil)
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

	if response["message"] != "Coach validated successfully" {
		t.Errorf("Expected success message, got %v", response["message"])
	}
}

func TestAdminHandler_ValidateCoach_NonAdminForbidden(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	adminHandler := handler.NewAdminHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AdminHandler: adminHandler,
	})

	_, userToken := testutil.CreateTestUser(t, queries, "regularuser@test.com")
	coachID, _ := testutil.CreateTestCoachUser(t, queries, "pendingcoach@test.com")

	req := testutil.NewJSONRequest(http.MethodPut, "/api/admin/coaches/"+coachID+"/validate", nil)
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

	if response["error"] != "Admin access required" {
		t.Errorf("Expected admin access error, got %v", response["error"])
	}
}

func TestAdminHandler_ValidateCoach_InvalidID(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	adminHandler := handler.NewAdminHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AdminHandler: adminHandler,
	})

	_, adminToken := testutil.CreateTestAdminUser(t, queries, "admin@test.com")

	req := testutil.NewJSONRequest(http.MethodPut, "/api/admin/coaches/invalid-uuid/validate", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(adminToken))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["error"] != "Invalid user ID" {
		t.Errorf("Expected invalid user ID error, got %v", response["error"])
	}
}

func TestAdminHandler_RejectCoach_Success(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	adminHandler := handler.NewAdminHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AdminHandler: adminHandler,
	})

	_, adminToken := testutil.CreateTestAdminUser(t, queries, "admin@test.com")
	coachID, _ := testutil.CreateTestCoachUser(t, queries, "rejectcoach@test.com")

	req := testutil.NewJSONRequest(http.MethodPut, "/api/admin/coaches/"+coachID+"/reject", nil)
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

	if response["message"] != "Coach rejected successfully" {
		t.Errorf("Expected success message, got %v", response["message"])
	}
}

func TestAdminHandler_RejectCoach_NonAdminForbidden(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	adminHandler := handler.NewAdminHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AdminHandler: adminHandler,
	})

	_, userToken := testutil.CreateTestUser(t, queries, "regularuser@test.com")
	coachID, _ := testutil.CreateTestCoachUser(t, queries, "rejectcoach@test.com")

	req := testutil.NewJSONRequest(http.MethodPut, "/api/admin/coaches/"+coachID+"/reject", nil)
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

	if response["error"] != "Admin access required" {
		t.Errorf("Expected admin access error, got %v", response["error"])
	}
}

func TestAdminHandler_RejectCoach_InvalidID(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	adminHandler := handler.NewAdminHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AdminHandler: adminHandler,
	})

	_, adminToken := testutil.CreateTestAdminUser(t, queries, "admin@test.com")

	req := testutil.NewJSONRequest(http.MethodPut, "/api/admin/coaches/invalid-uuid/reject", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(adminToken))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}

	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)

	if response["error"] != "Invalid user ID" {
		t.Errorf("Expected invalid user ID error, got %v", response["error"])
	}
}
