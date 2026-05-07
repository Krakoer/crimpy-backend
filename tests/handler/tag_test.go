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

func TestTagHandler_Create_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "tag1@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TagHandler: handler.NewTagHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "Strength", "color": "#FF0000"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/tags", body)
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

	if result["name"] != "Strength" {
		t.Errorf("Expected name 'Strength', got %v", result["name"])
	}
	if result["color"] != "#FF0000" {
		t.Errorf("Expected color '#FF0000', got %v", result["color"])
	}
}

func TestTagHandler_Create_MissingFields(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "tag2@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TagHandler: handler.NewTagHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "Strength"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/tags", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected %d for missing color, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

func TestTagHandler_Create_NotCoach(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "tag3@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TagHandler: handler.NewTagHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "Strength", "color": "#FF0000"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/tags", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestTagHandler_List(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "tag4@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TagHandler: handler.NewTagHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "Cardio", "color": "#00FF00"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/tags", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	app.Test(req)

	req = testutil.NewRequest(http.MethodGet, "/api/coach/tags", nil)
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
		t.Error("Expected at least one tag")
	}
}

func TestTagHandler_UpdateAndDelete(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "tag5@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TagHandler: handler.NewTagHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "Original", "color": "#111111"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/tags", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	tagID := created["id"].(string)

	body, _ = json.Marshal(map[string]interface{}{"name": "Updated", "color": "#222222"})
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/coach/tags/%s", tagID), body)
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
	if updated["color"] != "#222222" {
		t.Errorf("Expected color '#222222', got %v", updated["color"])
	}

	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/coach/tags/%s", tagID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d for delete, got %d", fiber.StatusOK, resp.StatusCode)
	}
}

func TestTagHandler_OwnershipIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token1 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "tag6@test.com")
	_, token2 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "tag7@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TagHandler: handler.NewTagHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "Coach1Tag", "color": "#AAAAAA"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/tags", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	tagID := created["id"].(string)

	body, _ = json.Marshal(map[string]interface{}{"name": "Hacked", "color": "#000000"})
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/coach/tags/%s", tagID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d for update isolation, got %d", fiber.StatusForbidden, resp.StatusCode)
	}

	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/coach/tags/%s", tagID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d for delete isolation, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestTagHandler_AssignAndUnassign(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "tag8@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
		TagHandler:      handler.NewTagHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "Squat"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)
	var exercise map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&exercise)
	exerciseID := exercise["id"].(string)

	body, _ = json.Marshal(map[string]interface{}{"name": "Legs", "color": "#0000FF"})
	req = testutil.NewJSONRequest(http.MethodPost, "/api/coach/tags", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)
	var tag map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tag)
	tagID := tag["id"].(string)

	// Assign tag to exercise
	req = testutil.NewRequest(http.MethodPost, fmt.Sprintf("/api/coach/exercises/%s/tags/%s", exerciseID, tagID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Assign request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d for assign, got %d", fiber.StatusOK, resp.StatusCode)
	}

	// Get exercise and verify tag is present
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/exercises/%s", exerciseID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	var exResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&exResp)
	tags := exResp["tags"].([]interface{})
	if len(tags) != 1 {
		t.Fatalf("Expected 1 tag on exercise, got %d", len(tags))
	}
	tagOnExercise := tags[0].(map[string]interface{})
	if tagOnExercise["id"] != tagID {
		t.Errorf("Expected tag ID %s, got %v", tagID, tagOnExercise["id"])
	}

	// Unassign tag
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/coach/exercises/%s/tags/%s", exerciseID, tagID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d for unassign, got %d", fiber.StatusOK, resp.StatusCode)
	}

	// Verify tag removed from exercise
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/exercises/%s", exerciseID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	json.NewDecoder(resp.Body).Decode(&exResp)
	tags = exResp["tags"].([]interface{})
	if len(tags) != 0 {
		t.Errorf("Expected 0 tags after unassign, got %d", len(tags))
	}
}

func TestTagHandler_Assign_CrossCoachIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token1 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "tag9@test.com")
	_, token2 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "tag10@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
		TagHandler:      handler.NewTagHandler(queries, pool),
	})

	// Coach 1 creates exercise and tag
	body, _ := json.Marshal(map[string]interface{}{"name": "Deadlift"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ := app.Test(req)
	var ex map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&ex)
	exerciseID := ex["id"].(string)

	body, _ = json.Marshal(map[string]interface{}{"name": "Power", "color": "#FF00FF"})
	req = testutil.NewJSONRequest(http.MethodPost, "/api/coach/tags", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ = app.Test(req)
	var tg map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tg)
	tagID := tg["id"].(string)

	// Coach 2 tries to assign coach 1's tag to coach 1's exercise
	req = testutil.NewRequest(http.MethodPost, fmt.Sprintf("/api/coach/exercises/%s/tags/%s", exerciseID, tagID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d for cross-coach assign, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestTagHandler_DeleteTag_RemovesFromExercise(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "tag11@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
		TagHandler:      handler.NewTagHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "Bench"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/exercises", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)
	var ex map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&ex)
	exerciseID := ex["id"].(string)

	body, _ = json.Marshal(map[string]interface{}{"name": "Upper", "color": "#123456"})
	req = testutil.NewJSONRequest(http.MethodPost, "/api/coach/tags", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)
	var tg map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tg)
	tagID := tg["id"].(string)

	// Assign
	req = testutil.NewRequest(http.MethodPost, fmt.Sprintf("/api/coach/exercises/%s/tags/%s", exerciseID, tagID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	app.Test(req)

	// Delete tag (cascade should remove from exercise_tags)
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/coach/tags/%s", tagID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	app.Test(req)

	// Exercise should have 0 tags
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/exercises/%s", exerciseID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	var exResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&exResp)
	tags := exResp["tags"].([]interface{})
	if len(tags) != 0 {
		t.Errorf("Expected 0 tags after tag deletion, got %d", len(tags))
	}
}
