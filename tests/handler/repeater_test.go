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

func TestRepeaterHandler_CreateRepeater_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "repeater1@test.com")

	repeaterHandler := handler.NewRepeaterHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		RepeaterHandler: repeaterHandler,
	})

	targetWeightRight := float32(50.5)
	targetWeightLeft := float32(48.0)

	reqBody := map[string]interface{}{
		"sets":                5,
		"reps":                10,
		"worktime":            7,
		"resttime":            3,
		"set_rest":            180,
		"target_weight_right": targetWeightRight,
		"target_weight_left":  targetWeightLeft,
		"split_hand":          true,
		"grip_position":       20,
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/repeaters", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected status %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var repeater map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&repeater); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if int(repeater["Sets"].(float64)) != 5 {
		t.Errorf("Expected sets 5, got %v", repeater["Sets"])
	}

	if int(repeater["Reps"].(float64)) != 10 {
		t.Errorf("Expected reps 10, got %v", repeater["Reps"])
	}

	if repeater["SplitHand"] != true {
		t.Errorf("Expected split_hand true, got %v", repeater["SplitHand"])
	}
}

func TestRepeaterHandler_CreateRepeater_InvalidData(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "repeater2@test.com")

	repeaterHandler := handler.NewRepeaterHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		RepeaterHandler: repeaterHandler,
	})

	// Invalid JSON
	req := testutil.NewRequest(http.MethodPost, "/api/repeaters", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

func TestRepeaterHandler_CreateRepeater_Unauthorized(t *testing.T) {
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	repeaterHandler := handler.NewRepeaterHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		RepeaterHandler: repeaterHandler,
	})

	reqBody := map[string]interface{}{
		"sets":          5,
		"reps":          10,
		"worktime":      7,
		"resttime":      3,
		"set_rest":      180,
		"split_hand":    false,
		"grip_position": 20,
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/repeaters", body)
	// No authorization header

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", fiber.StatusUnauthorized, resp.StatusCode)
	}
}

func TestRepeaterHandler_GetRepeater_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "repeater3@test.com")

	repeaterHandler := handler.NewRepeaterHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		RepeaterHandler: repeaterHandler,
	})

	// Create a repeater
	reqBody := map[string]interface{}{
		"sets":          5,
		"reps":          10,
		"worktime":      7,
		"resttime":      3,
		"set_rest":      180,
		"split_hand":    false,
		"grip_position": 20,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/repeaters", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create repeater: %v", err)
	}

	var createdRepeater map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdRepeater)
	repeaterID := createdRepeater["ID"].(string)

	// Get the repeater
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/repeaters/%s", repeaterID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var repeater map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&repeater)

	if int(repeater["Sets"].(float64)) != 5 {
		t.Errorf("Expected sets 5, got %v", repeater["Sets"])
	}

	if int(repeater["Reps"].(float64)) != 10 {
		t.Errorf("Expected reps 10, got %v", repeater["Reps"])
	}
}

func TestRepeaterHandler_GetRepeater_NotFound(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "repeater4@test.com")

	repeaterHandler := handler.NewRepeaterHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		RepeaterHandler: repeaterHandler,
	})

	// Try to get a non-existent repeater
	req := testutil.NewRequest(http.MethodGet, "/api/repeaters/00000000-0000-0000-0000-000000000000", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected status %d, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}

func TestRepeaterHandler_UpdateRepeater_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "repeater5@test.com")

	repeaterHandler := handler.NewRepeaterHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		RepeaterHandler: repeaterHandler,
	})

	// Create a repeater
	reqBody := map[string]interface{}{
		"sets":          5,
		"reps":          10,
		"worktime":      7,
		"resttime":      3,
		"set_rest":      180,
		"split_hand":    false,
		"grip_position": 20,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/repeaters", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create repeater: %v", err)
	}

	var createdRepeater map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdRepeater)
	repeaterID := createdRepeater["ID"].(string)

	// Update the repeater
	updateBody := map[string]interface{}{
		"sets":          8,
		"reps":          12,
		"worktime":      10,
		"resttime":      5,
		"set_rest":      240,
		"split_hand":    true,
		"grip_position": 25,
	}
	body, _ = json.Marshal(updateBody)
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/repeaters/%s", repeaterID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var updatedRepeater map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updatedRepeater)

	if int(updatedRepeater["Sets"].(float64)) != 8 {
		t.Errorf("Expected sets 8, got %v", updatedRepeater["Sets"])
	}

	if int(updatedRepeater["Reps"].(float64)) != 12 {
		t.Errorf("Expected reps 12, got %v", updatedRepeater["Reps"])
	}

	if updatedRepeater["SplitHand"] != true {
		t.Errorf("Expected split_hand true, got %v", updatedRepeater["SplitHand"])
	}
}

func TestRepeaterHandler_UpdateRepeater_NotFound(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "repeater6@test.com")

	repeaterHandler := handler.NewRepeaterHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		RepeaterHandler: repeaterHandler,
	})

	updateBody := map[string]interface{}{
		"sets":          8,
		"reps":          12,
		"worktime":      10,
		"resttime":      5,
		"set_rest":      240,
		"split_hand":    true,
		"grip_position": 25,
	}
	body, _ := json.Marshal(updateBody)
	req := testutil.NewJSONRequest(http.MethodPut, "/api/repeaters/00000000-0000-0000-0000-000000000000", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", fiber.StatusInternalServerError, resp.StatusCode)
	}
}

func TestRepeaterHandler_DeleteRepeater_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "repeater7@test.com")

	repeaterHandler := handler.NewRepeaterHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		RepeaterHandler: repeaterHandler,
	})

	// Create a repeater
	reqBody := map[string]interface{}{
		"sets":          5,
		"reps":          10,
		"worktime":      7,
		"resttime":      3,
		"set_rest":      180,
		"split_hand":    false,
		"grip_position": 20,
	}
	body, _ := json.Marshal(reqBody)
	req := testutil.NewJSONRequest(http.MethodPost, "/api/repeaters", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create repeater: %v", err)
	}

	var createdRepeater map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createdRepeater)
	repeaterID := createdRepeater["ID"].(string)

	// Delete the repeater
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/repeaters/%s", repeaterID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	// Try to get the deleted repeater
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/repeaters/%s", repeaterID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected status %d after deletion, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}

func TestRepeaterHandler_DeleteRepeater_NotFound(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "repeater8@test.com")

	repeaterHandler := handler.NewRepeaterHandler(queries)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		RepeaterHandler: repeaterHandler,
	})

	// Try to delete a non-existent repeater
	req := testutil.NewRequest(http.MethodDelete, "/api/repeaters/00000000-0000-0000-0000-000000000000", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	// DELETE operations return 200 even if the repeater doesn't exist (PostgreSQL behavior)
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status %d, got %d", fiber.StatusOK, resp.StatusCode)
	}
}
