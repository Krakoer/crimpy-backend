package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestCoachTrainingHandler_Create_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "ctraining1@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
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

	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
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

func TestCoachTrainingHandler_GetWithItems(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "ctraining3@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Strength",
		"items": []map[string]interface{}{
			{
				"type":        "group",
				"group_title": "Warm-up",
				"items": []map[string]interface{}{
					{
						"type": "exercise",
						"reps": 5,
					},
				},
			},
		},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	trainingID := created["id"].(string)

	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/trainings/%s", trainingID), nil)
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
		t.Fatalf("Expected 1 group, got %d", len(items))
	}

	group := items[0].(map[string]interface{})
	if group["type"] != "group" {
		t.Errorf("Expected type 'group', got %v", group["type"])
	}
	if group["group_title"] != "Warm-up" {
		t.Errorf("Expected group_title 'Warm-up', got %v", group["group_title"])
	}

	children := group["items"].([]interface{})
	if len(children) != 1 {
		t.Errorf("Expected 1 child exercise, got %d", len(children))
	}
}

func TestCoachTrainingHandler_OwnershipIsolation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token1 := testutil.CreateTestUser(t, queries, "ctraining4@test.com")
	_, token2 := testutil.CreateTestUser(t, queries, "ctraining5@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"title": "Coach1 Training"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token1))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	trainingID := created["id"].(string)

	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/trainings/%s", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token2))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d for get isolation, got %d", fiber.StatusForbidden, resp.StatusCode)
	}

	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/trainings/%s", trainingID), nil)
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

	_, token := testutil.CreateTestUser(t, queries, "ctraining6@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Original",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps": 5},
		},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
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
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/trainings/%s", trainingID), body)
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

	_, token := testutil.CreateTestUser(t, queries, "ctraining8@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
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
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
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
	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/trainings/%s", trainingID), nil)
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

	_, token := testutil.CreateTestUser(t, queries, "ctraining7@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{"title": "To Delete"})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ := app.Test(req)

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	trainingID := created["id"].(string)

	req = testutil.NewRequest(http.MethodDelete, fmt.Sprintf("/api/trainings/%s", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/trainings/%s", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, _ = app.Test(req)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected %d after delete, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}

func TestCoachTrainingHandler_Delete_ScheduledByProgram(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "ctraining8coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "ctraining8user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	weekBody, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
		},
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), weekBody, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to schedule the training: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 scheduling the training, got %d", resp.StatusCode)
	}

	req = testutil.NewRequestWithAuth(http.MethodDelete, fmt.Sprintf("/api/trainings/%s", trainingID), nil, coachToken)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Delete request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("Expected %d deleting a scheduled training, got %d", fiber.StatusConflict, resp.StatusCode)
	}

	var conflict map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&conflict)

	message, _ := conflict["error"].(string)
	if !strings.Contains(message, "Test Program") {
		t.Errorf("Expected the error to name the program, got %q", message)
	}

	programs, ok := conflict["programs"].([]interface{})
	if !ok || len(programs) != 1 {
		t.Fatalf("Expected 1 program in the conflict body, got %v", conflict["programs"])
	}
	program := programs[0].(map[string]interface{})
	if program["id"] != programID {
		t.Errorf("Expected program id %s, got %v", programID, program["id"])
	}
	if program["name"] != "Test Program" {
		t.Errorf("Expected program name 'Test Program', got %v", program["name"])
	}
	if program["session_count"] != float64(1) {
		t.Errorf("Expected session_count 1, got %v", program["session_count"])
	}

	req = testutil.NewRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/trainings/%s", trainingID), nil, coachToken)
	resp, _ = app.Test(req)
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected the training to survive the refused delete, got %d", resp.StatusCode)
	}
}
