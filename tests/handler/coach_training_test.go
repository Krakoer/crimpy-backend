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

func TestCoachTrainingHandler_Create_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "ctraining1@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title":       "Warm-up",
		"description": "Full body warm-up",
		"items": []map[string]interface{}{
			{
				"type":               "circuit",
				"cycles":             2,
				"cycle_rest_seconds": 30,
				"items": []map[string]interface{}{
					{
						"type":         "exercise",
						"reps":         10,
						"rest_seconds": 0,
						"loads":        []map[string]interface{}{{"value": 0, "unit": "bw"}},
					},
				},
			},
		},
	})

	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/trainings", body)
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

	if result["title"] != "Warm-up" {
		t.Errorf("Expected title 'Warm-up', got %v", result["title"])
	}
	if result["training_type"] != "workout" {
		t.Errorf("Expected default training_type 'workout', got %v", result["training_type"])
	}

	items, ok := result["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Errorf("Expected 1 top-level item, got %v", result["items"])
	}

	circuit := items[0].(map[string]interface{})
	if circuit["type"] != "circuit" {
		t.Errorf("Expected type 'circuit', got %v", circuit["type"])
	}

	children := circuit["items"].([]interface{})
	if len(children) != 1 {
		t.Errorf("Expected 1 child item, got %d", len(children))
	}
}

func TestCoachTrainingHandler_Create_NotCoach(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "ctraining2@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"title": "Training"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, _ := app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestCoachTrainingHandler_GetWithItems(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "ctraining3@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Strength",
		"items": []map[string]interface{}{
			{
				"type":          "section",
				"section_title": "Warm-up",
				"items": []map[string]interface{}{
					{
						"type": "exercise",
						"reps": 5,
					},
				},
			},
		},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	trainingID := created["id"].(string)

	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/trainings/%s", trainingID), nil)
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

	items := result["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("Expected 1 section, got %d", len(items))
	}

	section := items[0].(map[string]interface{})
	if section["type"] != "section" {
		t.Errorf("Expected type 'section', got %v", section["type"])
	}
	if section["section_title"] != "Warm-up" {
		t.Errorf("Expected section_title 'Warm-up', got %v", section["section_title"])
	}

	children := section["items"].([]interface{})
	if len(children) != 1 {
		t.Errorf("Expected 1 child exercise, got %d", len(children))
	}
}

func TestCoachTrainingHandler_OwnershipIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token1 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "ctraining4@test.com")
	_, token2 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "ctraining5@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"title": "Coach1 Training"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	trainingID := created["id"].(string)

	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/trainings/%s", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d for get isolation, got %d", fiber.StatusForbidden, resp.StatusCode)
	}

	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/coach/trainings/%s", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d for delete isolation, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestCoachTrainingHandler_Update(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "ctraining6@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Original",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps": 5},
		},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	trainingID := created["id"].(string)

	body, _ = json.Marshal(map[string]interface{}{
		"title":   "Updated",
		"goal":    "Build endurance",
		"comment": "Max RPE 8",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps": 10},
			{"type": "exercise", "reps": 15},
		},
	})
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/coach/trainings/%s", trainingID), body)
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

	if result["title"] != "Updated" {
		t.Errorf("Expected title 'Updated', got %v", result["title"])
	}
	if result["goal"] != "Build endurance" {
		t.Errorf("Expected goal 'Build endurance', got %v", result["goal"])
	}
	if result["comment"] != "Max RPE 8" {
		t.Errorf("Expected comment 'Max RPE 8', got %v", result["comment"])
	}

	items := result["items"].([]interface{})
	if len(items) != 2 {
		t.Errorf("Expected 2 items after update, got %d", len(items))
	}
}

func TestCoachTrainingHandler_StretchingType(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "ctraining8@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title":         "Hip Mobility",
		"training_type": "stretching",
		"goal":          "Improve hip flexibility",
		"comment":       "Hold each stretch 30s",
		"items": []map[string]interface{}{
			{
				"type":   "circuit",
				"cycles": 1,
				"items": []map[string]interface{}{
					{"type": "exercise", "duration": 30},
				},
			},
		},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)

	if created["training_type"] != "stretching" {
		t.Errorf("Expected training_type 'stretching', got %v", created["training_type"])
	}
	if created["goal"] != "Improve hip flexibility" {
		t.Errorf("Expected goal 'Improve hip flexibility', got %v", created["goal"])
	}
	if created["comment"] != "Hold each stretch 30s" {
		t.Errorf("Expected comment 'Hold each stretch 30s', got %v", created["comment"])
	}

	trainingID := created["id"].(string)
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/trainings/%s", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	var fetched map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&fetched)

	if fetched["training_type"] != "stretching" {
		t.Errorf("Expected training_type 'stretching' from GET, got %v", fetched["training_type"])
	}
}

func TestCoachTrainingHandler_Delete(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "ctraining7@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"title": "To Delete"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	trainingID := created["id"].(string)

	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/coach/trainings/%s", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/trainings/%s", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected %d after delete, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}
