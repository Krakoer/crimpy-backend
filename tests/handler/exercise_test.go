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

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	exercises, ok := result["exercises"].([]interface{})
	if !ok || len(exercises) == 0 {
		t.Error("Expected at least one exercise in paginated response")
	}
	if result["total"] == nil {
		t.Error("Expected total field in paginated response")
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

func TestExerciseHandler_Favorite_SetAndList(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "exercisefav1@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
	})

	// Create two exercises
	body, _ := json.Marshal(map[string]interface{}{"name": "Deadlift"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)
	var ex1 map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&ex1)
	ex1ID := ex1["id"].(string)

	body, _ = json.Marshal(map[string]interface{}{"name": "Pull-up"})
	req = testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	app.Test(req)

	// Mark ex1 as favorite
	body, _ = json.Marshal(map[string]interface{}{"is_favorite": true})
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/coach/exercises/%s/favorite", ex1ID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var updated map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updated)
	if updated["is_favorite"] != true {
		t.Errorf("Expected is_favorite to be true, got %v", updated["is_favorite"])
	}

	// List favorites - should contain only ex1
	req = testutil.NewRequest(http.MethodGet, "/api/coach/exercises/favorites", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var favorites []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&favorites)
	if len(favorites) != 1 {
		t.Errorf("Expected 1 favorite, got %d", len(favorites))
	}
	if favorites[0]["id"] != ex1ID {
		t.Errorf("Expected favorite to be ex1, got %v", favorites[0]["id"])
	}

	// Unmark favorite
	body, _ = json.Marshal(map[string]interface{}{"is_favorite": false})
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/coach/exercises/%s/favorite", ex1ID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	var unmarked map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&unmarked)
	if unmarked["is_favorite"] != false {
		t.Errorf("Expected is_favorite to be false after unmark, got %v", unmarked["is_favorite"])
	}

	// Favorites list should now be empty
	req = testutil.NewRequest(http.MethodGet, "/api/coach/exercises/favorites", nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	var emptyFavs []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&emptyFavs)
	if len(emptyFavs) != 0 {
		t.Errorf("Expected 0 favorites after unmark, got %d", len(emptyFavs))
	}
}

func TestExerciseHandler_Favorite_OwnershipIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token1 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "exercisefav2@test.com")
	_, token2 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "exercisefav3@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
	})

	// Coach 1 creates an exercise
	body, _ := json.Marshal(map[string]interface{}{"name": "Squat"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ := app.Test(req)
	var ex map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&ex)
	exID := ex["id"].(string)

	// Coach 2 tries to favorite it
	body, _ = json.Marshal(map[string]interface{}{"is_favorite": true})
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/coach/exercises/%s/favorite", exID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}
