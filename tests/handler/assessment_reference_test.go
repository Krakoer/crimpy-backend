package handler_test

import (
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// A reps target had no assessment it could validly reference while the only
// assessments were the three Crimpy ships, none of which counts repetitions. A
// custom one in repetitions is what makes that path reachable at all.
func TestAssessmentReference_RepsTargetAgainstCustomAssessment(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessref1@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, token, "Pull up max", "repetitions", false)

	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Volume day",
		"items": []map[string]interface{}{
			{
				"type": "exercise",
				"reps": 8,
				"variable_targets": map[string]interface{}{
					"reps": map[string]interface{}{
						"assessment_id": assessmentID, "percent": 60, "fallback": 8,
					},
				},
			},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 for a reps target on a repetitions assessment, got %d: %v", status, training)
	}

	items := training["items"].([]interface{})
	item := items[0].(map[string]interface{})
	targets := item["variable_targets"].(map[string]interface{})
	reps := targets["reps"].(map[string]interface{})
	if reps["assessment_id"] != assessmentID {
		t.Errorf("Expected the reference to survive, got %v", reps["assessment_id"])
	}

	// The definition rides along, so a client reading the tree can name the
	// reference without a request of its own.
	referenced := training["referenced_assessments"].([]interface{})
	if len(referenced) != 1 {
		t.Fatalf("Expected the referenced assessment named, got %v", referenced)
	}
	named := referenced[0].(map[string]interface{})
	if named["id"] != assessmentID || named["unit"] != "repetitions" {
		t.Errorf("Expected the reps assessment named with its unit, got %v", named)
	}
}

// The unit is what stops a force result prescribing a hang time, and it has to
// keep doing so now that it comes from a row rather than a compiled table.
func TestAssessmentReference_RejectsUnitMismatch(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessref2@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, token, "Pull up max", "repetitions", false)

	status, _ := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Wrong unit",
		"items": []map[string]interface{}{
			{
				"type":     "exercise",
				"duration": 60,
				"variable_targets": map[string]interface{}{
					"duration": map[string]interface{}{
						"assessment_id": assessmentID, "percent": 75, "fallback": 60,
					},
				},
			},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d driving a duration from a repetitions assessment, got %d", fiber.StatusBadRequest, status)
	}
}

// Ownership of a reference is enforced by the query that resolves it, so a
// training may not name an assessment its writer cannot reference.
func TestAssessmentReference_RejectsForeignAssessment(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, ownerToken := testutil.CreateTestUser(t, queries, "assessref3@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "assessref4@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, ownerToken, "Private test", "seconds", false)

	status, _ := postJSON(t, app, "/api/trainings", otherToken, map[string]interface{}{
		"title": "Borrowed reference",
		"items": []map[string]interface{}{
			{
				"type":     "exercise",
				"duration": 30,
				"variable_targets": map[string]interface{}{
					"duration": map[string]interface{}{
						"assessment_id": assessmentID, "percent": 50, "fallback": 30,
					},
				},
			},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d referencing another user's assessment, got %d", fiber.StatusBadRequest, status)
	}
}

// A result is recorded against an assessment, and an assessment the athlete
// cannot reach is refused the same way an unknown one is.
func TestAssessmentReference_RecordsAndListsCustomResult(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessref5@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, token, "One arm lock off", "seconds", true)

	status, _ := postJSON(t, app, "/api/sessions", token, map[string]interface{}{
		"name":          "One arm lock off",
		"notes":         "",
		"activity":      0,
		"origin":        "played",
		"duration":      120,
		"is_assessment": true,
		"assessments": []map[string]interface{}{
			{"assessment_id": assessmentID, "right_value": 3, "left_value": 6},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording a custom result, got %d", status)
	}

	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/assessments", nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var list []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("Expected the one result, got %v", list)
	}

	result := list[0]
	if result["assessment_id"] != assessmentID {
		t.Errorf("Expected the result to name the assessment, got %v", result["assessment_id"])
	}
	// The definition rides on the row, so a profile section can be rendered
	// without a second request.
	if result["label"] != "One arm lock off" || result["unit"] != "seconds" || result["per_hand"] != true {
		t.Errorf("Expected the assessment named, measured and flagged per hand, got %v", result)
	}
	if result["right_value"] != float64(3) || result["left_value"] != float64(6) {
		t.Errorf("Expected the two hands kept apart, got %v", result)
	}
}

func TestAssessmentReference_RejectsUnknownAssessmentOnResult(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, ownerToken := testutil.CreateTestUser(t, queries, "assessref6@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "assessref7@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, ownerToken, "Private test", "seconds", false)

	status, _ := postJSON(t, app, "/api/sessions", otherToken, map[string]interface{}{
		"name":          "Not mine to measure",
		"notes":         "",
		"activity":      0,
		"origin":        "played",
		"duration":      60,
		"is_assessment": true,
		"assessments": []map[string]interface{}{
			{"assessment_id": assessmentID, "right_value": 10},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d recording against another user's assessment, got %d", fiber.StatusBadRequest, status)
	}
}

// The unit says what every past number means, so it cannot move once results
// exist. The label is free to change: it names the assessment, not its results.
func TestAssessmentDefinitions_FreezesUnitOnceMeasured(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessref8@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, token, "Pull up pyramid", "repetitions", false)

	status, _ := postJSON(t, app, "/api/sessions", token, map[string]interface{}{
		"name": "Pyramid", "notes": "", "activity": 0, "origin": "played",
		"duration": 120, "is_assessment": true,
		"assessments": []map[string]interface{}{
			{"assessment_id": assessmentID, "right_value": 14},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d", status)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"label": "Pull up pyramid", "prompt": "How many?", "unit": "seconds",
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/assessment-definitions/"+assessmentID, body, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected %d changing the unit of a measured assessment, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}

	body, _ = json.Marshal(map[string]interface{}{
		"label": "Pull up pyramid to failure", "prompt": "How many?", "unit": "repetitions",
	})
	resp, err = app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/assessment-definitions/"+assessmentID, body, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected a rename to be allowed, got %d", resp.StatusCode)
	}
}

// A history must never be silently erased, so the definition holding it refuses
// to go.
func TestAssessmentDefinitions_RefusesDeleteWithResults(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessref9@test.com")
	app := assessmentApp(t, pool, queries)

	trainingID, assessmentID := createAssessmentTraining(t, app, token, "Pull up pyramid", "repetitions", false)

	status, _ := postJSON(t, app, "/api/sessions", token, map[string]interface{}{
		"name": "Pyramid", "notes": "", "activity": 0, "origin": "played",
		"duration": 120, "is_assessment": true,
		"assessments": []map[string]interface{}{
			{"assessment_id": assessmentID, "right_value": 14},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d", status)
	}

	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodDelete, "/api/assessment-definitions/"+assessmentID, nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected %d deleting a measured assessment, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}

	// The training it is run from cannot go either: that would leave the
	// assessment with nothing to measure.
	resp, err = app.Test(testutil.NewJSONRequestWithAuth(http.MethodDelete, "/api/trainings/"+trainingID, nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("Expected %d deleting the training an assessment measures, got %d", fiber.StatusConflict, resp.StatusCode)
	}
}
