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

func TestExerciseHandler_Create_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "exercise1@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"name":        "Pull-up",
		"description": "Upper body pull",
		"comment":     "Keep elbows tucked",
		"video_link":  "https://example.com/pullup",
	})

	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if result["name"] != "Pull-up" {
		t.Errorf("Expected name 'Pull-up', got %v", result["name"])
	}
	if result["description"] != "Upper body pull" {
		t.Errorf("Expected description, got %v", result["description"])
	}
}

func TestExerciseHandler_Create_NotCoach(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "exercise2@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "Pull-up"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, _ := app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestExerciseHandler_Create_UnvalidatedCoach(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestCoachUser(t, queries, "exercise3@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "Pull-up"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, _ := app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestExerciseHandler_List_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "exercise4@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
	})

	// Create an exercise first
	body, _ := json.Marshal(map[string]interface{}{"name": "Push-up"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	app.Test(req)

	// List exercises
	req = testutil.NewRequest(http.MethodGet, "/api/coach/exercises", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var result []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result) == 0 {
		t.Error("Expected at least one exercise")
	}
}

func TestExerciseHandler_OwnershipIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token1 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "exercise5@test.com")
	_, token2 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "exercise6@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
	})

	// Coach 1 creates an exercise
	body, _ := json.Marshal(map[string]interface{}{"name": "Deadlift"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	exerciseID := created["id"].(string)

	// Coach 2 tries to access it
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/exercises/%s", exerciseID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}

	// Coach 2 tries to delete it
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/coach/exercises/%s", exerciseID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d for delete isolation, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestExerciseHandler_UpdateAndDelete(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "exercise7@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
	})

	// Create
	body, _ := json.Marshal(map[string]interface{}{"name": "Original"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	exerciseID := created["id"].(string)

	// Update
	body, _ = json.Marshal(map[string]interface{}{"name": "Updated", "comment": "new comment"})
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/coach/exercises/%s", exerciseID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d for update, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var updated map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updated)
	if updated["name"] != "Updated" {
		t.Errorf("Expected name 'Updated', got %v", updated["name"])
	}

	// Delete
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/coach/exercises/%s", exerciseID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d for delete, got %d", fiber.StatusOK, resp.StatusCode)
	}

	// Verify gone
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/exercises/%s", exerciseID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected %d after delete, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}
