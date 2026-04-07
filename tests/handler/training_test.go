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

func TestTrainingHandler_CreateTraining_Success(t *testing.T) {
	// Set JWT secret for testing
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	// Create test user and get token
	userID, token := testutil.CreateTestUser(t, queries, "training1@test.com")

	trainingHandler := handler.NewTrainingHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: trainingHandler,
	})

	// Create training
	reqBody := map[string]interface{}{
		"name":          "Test Training",
		"is_favorite":   true,
		"is_assessment": false,
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var training map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&training)

	if training["Name"] != "Test Training" {
		t.Errorf("Expected name 'Test Training', got %v", training["Name"])
	}

	if training["UserID"] != userID {
		t.Errorf("Expected user_id '%s', got %v", userID, training["UserID"])
	}
}

func TestTrainingHandler_CreateTraining_MissingName(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "training2@test.com")

	trainingHandler := handler.NewTrainingHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: trainingHandler,
	})

	reqBody := map[string]interface{}{
		"is_favorite": true,
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

func TestTrainingHandler_CreateTraining_Unauthorized(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	trainingHandler := handler.NewTrainingHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: trainingHandler,
	})

	reqBody := map[string]interface{}{
		"name": "Test Training",
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	// No authorization header

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", fiber.StatusUnauthorized, resp.StatusCode)
	}
}

func TestTrainingHandler_GetTrainings_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "training3@test.com")

	trainingHandler := handler.NewTrainingHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: trainingHandler,
	})

	// Create a training first
	reqBody := map[string]interface{}{
		"name":        "Test Training",
		"is_favorite": true,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	_, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create training: %v", err)
	}

	// Get trainings
	req = testutil.NewRequest(http.MethodGet, "/api/trainings", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var trainings []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&trainings)

	if len(trainings) == 0 {
		t.Error("Expected at least one training")
	}
}

func TestTrainingHandler_GetTraining_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "training4@test.com")

	trainingHandler := handler.NewTrainingHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: trainingHandler,
	})

	// Create a training
	reqBody := map[string]interface{}{
		"name":        "Test Training",
		"is_favorite": true,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create training: %v", err)
	}

	var createdTraining map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdTraining)
	trainingID := int(createdTraining["ID"].(float64))

	// Get the training
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/trainings/%d", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var training map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&training)

	if training["Name"] != "Test Training" {
		t.Errorf("Expected name 'Test Training', got %v", training["Name"])
	}
}

func TestTrainingHandler_GetTraining_UserIsolation(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	// Create two users
	_, token1 := testutil.CreateTestUser(t, queries, "training5@test.com")
	_, token2 := testutil.CreateTestUser(t, queries, "training6@test.com")

	trainingHandler := handler.NewTrainingHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: trainingHandler,
	})

	// User 1 creates a training
	reqBody := map[string]interface{}{
		"name":        "User 1 Training",
		"is_favorite": true,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create training: %v", err)
	}

	var createdTraining map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdTraining)
	trainingID := int(createdTraining["ID"].(float64))

	// User 2 tries to access User 1's training
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/trainings/%d", trainingID), nil)
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

func TestTrainingHandler_UpdateTraining_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "training7@test.com")

	trainingHandler := handler.NewTrainingHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: trainingHandler,
	})

	// Create a training
	reqBody := map[string]interface{}{
		"name":        "Original Name",
		"is_favorite": false,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create training: %v", err)
	}

	var createdTraining map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdTraining)
	trainingID := int(createdTraining["ID"].(float64))

	// Update the training
	updateBody := map[string]interface{}{
		"name":        "Updated Name",
		"is_favorite": true,
	}
	body, _ = json.Marshal(updateBody)
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/trainings/%d", trainingID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var updatedTraining map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updatedTraining)

	if updatedTraining["Name"] != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got %v", updatedTraining["Name"])
	}

	if updatedTraining["IsFavorite"] != true {
		t.Errorf("Expected is_favorite to be true, got %v", updatedTraining["IsFavorite"])
	}
}

func TestTrainingHandler_UpdateTraining_UserIsolation(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	// Create two users
	_, token1 := testutil.CreateTestUser(t, queries, "training8@test.com")
	_, token2 := testutil.CreateTestUser(t, queries, "training9@test.com")

	trainingHandler := handler.NewTrainingHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: trainingHandler,
	})

	// User 1 creates a training
	reqBody := map[string]interface{}{
		"name":        "User 1 Training",
		"is_favorite": false,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create training: %v", err)
	}

	var createdTraining map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdTraining)
	trainingID := int(createdTraining["ID"].(float64))

	// User 2 tries to update User 1's training
	updateBody := map[string]interface{}{
		"name":        "Hacked Name",
		"is_favorite": true,
	}
	body, _ = json.Marshal(updateBody)
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/trainings/%d", trainingID), body)
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

func TestTrainingHandler_DeleteTraining_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "training10@test.com")

	trainingHandler := handler.NewTrainingHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: trainingHandler,
	})

	// Create a training
	reqBody := map[string]interface{}{
		"name":        "To Delete",
		"is_favorite": false,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create training: %v", err)
	}

	var createdTraining map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdTraining)
	trainingID := int(createdTraining["ID"].(float64))

	// Delete the training
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/trainings/%d", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	// Try to get the deleted training
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/trainings/%d", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected status %d after deletion, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}

func TestTrainingHandler_DeleteTraining_UserIsolation(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	// Create two users
	_, token1 := testutil.CreateTestUser(t, queries, "training11@test.com")
	_, token2 := testutil.CreateTestUser(t, queries, "training12@test.com")

	trainingHandler := handler.NewTrainingHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: trainingHandler,
	})

	// User 1 creates a training
	reqBody := map[string]interface{}{
		"name":        "User 1 Training",
		"is_favorite": false,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create training: %v", err)
	}

	var createdTraining map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdTraining)
	trainingID := int(createdTraining["ID"].(float64))

	// User 2 tries to delete User 1's training
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/trainings/%d", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	// Should be forbidden
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected status %d (Forbidden), got %d", fiber.StatusForbidden, resp.StatusCode)
	}

	// Verify training still exists for user 1
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/trainings/%d", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusOK {
		t.Error("Training should still exist for original owner")
	}
}
