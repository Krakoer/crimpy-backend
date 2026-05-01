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

func TestCoachSessionHandler_Create_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "csession1@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachSessionHandler: handler.NewCoachSessionHandler(queries, pool),
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
						"reps_unit":    "count",
						"rest_seconds": 0,
						"loads":        []map[string]interface{}{{"value": 0, "unit": "bw"}},
					},
				},
			},
		},
	})

	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/sessions", body)
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

func TestCoachSessionHandler_Create_NotCoach(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "csession2@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachSessionHandler: handler.NewCoachSessionHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"title": "Session"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, _ := app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestCoachSessionHandler_GetWithItems(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "csession3@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachSessionHandler: handler.NewCoachSessionHandler(queries, pool),
	})

	// Create session with nested items
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Strength",
		"items": []map[string]interface{}{
			{
				"type":          "section",
				"section_title": "Warm-up",
				"items": []map[string]interface{}{
					{
						"type":      "exercise",
						"reps":      5,
						"reps_unit": "count",
					},
				},
			},
		},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	sessionID := created["id"].(string)

	// Get session
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/sessions/%s", sessionID), nil)
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

func TestCoachSessionHandler_OwnershipIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token1 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "csession4@test.com")
	_, token2 := testutil.CreateTestValidatedCoachUser(t, pool, queries, "csession5@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachSessionHandler: handler.NewCoachSessionHandler(queries, pool),
	})

	// Coach 1 creates a session
	body, _ := json.Marshal(map[string]interface{}{"title": "Coach1 Session"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	sessionID := created["id"].(string)

	// Coach 2 tries to get it
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d for get isolation, got %d", fiber.StatusForbidden, resp.StatusCode)
	}

	// Coach 2 tries to delete it
	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/coach/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d for delete isolation, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestCoachSessionHandler_Update(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "csession6@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachSessionHandler: handler.NewCoachSessionHandler(queries, pool),
	})

	// Create with one item
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Original",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps": 5, "reps_unit": "count"},
		},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	sessionID := created["id"].(string)

	// Update with new title and two items
	body, _ = json.Marshal(map[string]interface{}{
		"title": "Updated",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps": 10, "reps_unit": "count"},
			{"type": "exercise", "reps": 15, "reps_unit": "seconds"},
		},
	})
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/coach/sessions/%s", sessionID), body)
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

	items := result["items"].([]interface{})
	if len(items) != 2 {
		t.Errorf("Expected 2 items after update, got %d", len(items))
	}
}

func TestCoachSessionHandler_Delete(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "csession7@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachSessionHandler: handler.NewCoachSessionHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"title": "To Delete"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/coach/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	sessionID := created["id"].(string)

	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/coach/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/coach/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected %d after delete, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}
