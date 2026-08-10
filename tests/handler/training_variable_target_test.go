package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// createTrainingWithItems posts a one-training payload and returns the response
// status along with the decoded body.
func createTrainingWithItems(t *testing.T, email string, items []map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, email)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Variable loads",
		"items": items,
	})

	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func TestTrainingVariableTargets_RoundTrip(t *testing.T) {
	status, result := createTrainingWithItems(t, "vartarget1@test.com", []map[string]interface{}{
		{
			"type":     "exercise",
			"duration": 60,
			"variable_targets": map[string]interface{}{
				"duration": map[string]interface{}{
					"assessment_type": 2,
					"percent":         75,
					"fallback":        60,
				},
			},
			"loads": []map[string]interface{}{
				{"value": 80, "unit": "percent_assessment", "assessment_type": 1, "fallback": 30},
			},
		},
	})

	if status != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d (%v)", fiber.StatusCreated, status, result)
	}

	items := result["items"].([]interface{})
	item := items[0].(map[string]interface{})

	targets, ok := item["variable_targets"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected variable_targets on the item, got %v", item["variable_targets"])
	}
	duration := targets["duration"].(map[string]interface{})
	if duration["assessment_type"] != float64(2) {
		t.Errorf("Expected assessment_type 2, got %v", duration["assessment_type"])
	}
	if duration["percent"] != float64(75) {
		t.Errorf("Expected percent 75, got %v", duration["percent"])
	}
	if duration["fallback"] != float64(60) {
		t.Errorf("Expected fallback 60, got %v", duration["fallback"])
	}

	loads := item["loads"].([]interface{})
	load := loads[0].(map[string]interface{})
	if load["unit"] != "percent_assessment" {
		t.Errorf("Expected unit percent_assessment, got %v", load["unit"])
	}
	if load["assessment_type"] != float64(1) {
		t.Errorf("Expected load assessment_type 1, got %v", load["assessment_type"])
	}
	if load["fallback"] != float64(30) {
		t.Errorf("Expected load fallback 30, got %v", load["fallback"])
	}
}

func TestTrainingVariableTargets_RejectsUnknownField(t *testing.T) {
	status, _ := createTrainingWithItems(t, "vartarget2@test.com", []map[string]interface{}{
		{
			"type": "exercise",
			"variable_targets": map[string]interface{}{
				"rest_seconds": map[string]interface{}{
					"assessment_type": 1, "percent": 50, "fallback": 10,
				},
			},
		},
	})

	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d for an unknown variable target field, got %d", fiber.StatusBadRequest, status)
	}
}

func TestTrainingVariableTargets_RejectsUnknownAssessmentType(t *testing.T) {
	status, _ := createTrainingWithItems(t, "vartarget3@test.com", []map[string]interface{}{
		{
			"type": "exercise",
			"variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{
					"assessment_type": 9, "percent": 50, "fallback": 10,
				},
			},
		},
	})

	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d for an unknown assessment type, got %d", fiber.StatusBadRequest, status)
	}
}

func TestTrainingVariableTargets_RejectsPercentAssessmentLoadWithoutReference(t *testing.T) {
	status, _ := createTrainingWithItems(t, "vartarget4@test.com", []map[string]interface{}{
		{
			"type":  "hangboard_rep",
			"loads": []map[string]interface{}{{"value": 80, "unit": "percent_assessment"}},
		},
	})

	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d for a load missing its assessment reference, got %d", fiber.StatusBadRequest, status)
	}
}

func TestTrainingVariableTargets_RejectsDurationDrivenByForceAssessment(t *testing.T) {
	status, _ := createTrainingWithItems(t, "vartarget6@test.com", []map[string]interface{}{
		{
			"type":     "exercise",
			"duration": 60,
			"variable_targets": map[string]interface{}{
				"duration": map[string]interface{}{
					"assessment_type": 1, "percent": 75, "fallback": 60,
				},
			},
		},
	})

	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d for a duration driven by a kilograms assessment, got %d", fiber.StatusBadRequest, status)
	}
}

func TestTrainingVariableTargets_RejectsLoadDrivenByDurationAssessment(t *testing.T) {
	status, _ := createTrainingWithItems(t, "vartarget7@test.com", []map[string]interface{}{
		{
			"type":  "hangboard_rep",
			"loads": []map[string]interface{}{{"value": 80, "unit": "percent_assessment", "assessment_type": 2, "fallback": 30}},
		},
	})

	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d for a load driven by a seconds assessment, got %d", fiber.StatusBadRequest, status)
	}
}

func TestTrainingVariableTargets_RejectsRepsTarget(t *testing.T) {
	status, _ := createTrainingWithItems(t, "vartarget8@test.com", []map[string]interface{}{
		{
			"type": "exercise",
			"reps": 10,
			"variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{
					"assessment_type": 2, "percent": 50, "fallback": 10,
				},
			},
		},
	})

	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d for a reps target, nothing being measured in repetitions, got %d", fiber.StatusBadRequest, status)
	}
}

func TestTrainingVariableTargets_AcceptsSplitHandPerHandLoads(t *testing.T) {
	status, result := createTrainingWithItems(t, "vartarget5@test.com", []map[string]interface{}{
		{
			"type":        "repeater",
			"hand":        "split",
			"reps":        2,
			"granularity": "rep",
			"loads": []map[string]interface{}{
				{"value": 80, "unit": "percent_assessment", "assessment_type": 1, "fallback": 30},
				{"value": 0, "unit": "bw"},
			},
			"left_loads": []map[string]interface{}{
				{"value": 70, "unit": "percent_assessment", "assessment_type": 1, "fallback": 25},
				{"value": 0, "unit": "bw"},
			},
		},
	})

	if status != fiber.StatusCreated {
		t.Errorf("Expected %d for per-hand split-hand loads, got %d (%v)", fiber.StatusCreated, status, result)
	}
}
