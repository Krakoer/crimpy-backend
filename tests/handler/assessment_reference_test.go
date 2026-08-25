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
	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("Expected %d changing the unit of a measured assessment, got %d", fiber.StatusConflict, resp.StatusCode)
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
	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("Expected %d deleting a measured assessment, got %d", fiber.StatusConflict, resp.StatusCode)
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

// A load that is not a percentage of anything has no business naming an
// assessment. Collecting an id from one anyway resolved a reference nothing had
// checked the caller may name, which handed back another user's definition.
func TestAssessmentReference_IgnoresAnIdOnANonPercentLoad(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, ownerToken := testutil.CreateTestUser(t, queries, "assessleak1@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "assessleak2@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, ownerToken, "Private test", "seconds", true)

	status, training := postJSON(t, app, "/api/trainings", otherToken, map[string]interface{}{
		"title": "Probe",
		"items": []map[string]interface{}{
			{
				"type": "exercise",
				"reps": 5,
				// A plain kilogram load, so nothing validates the id beside it.
				"loads": []map[string]interface{}{
					{"unit": "kg", "value": 10, "assessment_id": assessmentID},
				},
			},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected the training to be accepted, got %d: %v", status, training)
	}

	// The stray id resolves to nothing, so no definition is named back.
	if referenced, present := training["referenced_assessments"]; present && referenced != nil {
		t.Errorf("Expected no assessment named from a non percent load, got %v", referenced)
	}
}

// A target may only reference an assessment measured in the unit of the field it
// drives. Moving the unit afterwards would leave the reference standing but no
// longer resolving, so the coach would read a prescription that quietly uses its
// fallback.
func TestAssessmentDefinitions_FreezesUnitWhileReferenced(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessfreeze@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, token, "Lock off", "seconds", false)

	status, _ := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Lock off work",
		"items": []map[string]interface{}{
			{
				"type":     "exercise",
				"duration": 10,
				"variable_targets": map[string]interface{}{
					"duration": map[string]interface{}{
						"assessment_id": assessmentID, "percent": 50, "fallback": 10,
					},
				},
			},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the referencing training, got %d", status)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"label": "Lock off", "prompt": "How long?", "unit": "repetitions",
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/assessment-definitions/"+assessmentID, body, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("Expected %d changing the unit of a referenced assessment, got %d", fiber.StatusConflict, resp.StatusCode)
	}

	// A rename is still fine: it names the assessment, it does not say what its
	// numbers mean.
	body, _ = json.Marshal(map[string]interface{}{
		"label": "One arm lock off", "prompt": "How long?", "unit": "seconds",
	})
	resp, err = app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/assessment-definitions/"+assessmentID, body, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected a rename to be allowed, got %d", resp.StatusCode)
	}
}

// Deleting a definition a training reads against would leave that training
// holding a reference nothing answers, refusing its next save.
func TestAssessmentDefinitions_RefusesDeleteWhileReferenced(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessrefdel@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, token, "Pull up pyramid", "repetitions", false)

	status, _ := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
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
		t.Fatalf("Expected 201, got %d", status)
	}

	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodDelete, "/api/assessment-definitions/"+assessmentID, nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("Expected %d deleting a referenced assessment, got %d", fiber.StatusConflict, resp.StatusCode)
	}
}

// One assessment per training, so a retry or a double tap is a refusal rather
// than a server fault.
func TestAssessmentDefinitions_RefusesASecondDeclaration(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessdup@test.com")
	app := assessmentApp(t, pool, queries)

	trainingID, _ := createAssessmentTraining(t, app, token, "Pull up pyramid", "repetitions", false)

	status, _ := postJSON(t, app, "/api/assessment-definitions", token, map[string]interface{}{
		"training_id": trainingID,
		"label":       "Pull up pyramid again",
		"prompt":      "How many?",
		"unit":        "repetitions",
	})
	if status != fiber.StatusConflict {
		t.Errorf("Expected %d declaring the same training twice, got %d", fiber.StatusConflict, status)
	}
}

// The filter is a three state bool, so each state is worth pinning.
func TestTrainings_FilterOnIsAssessment(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessfilter@test.com")
	app := assessmentApp(t, pool, queries)

	createAssessmentTraining(t, app, token, "Pull up pyramid", "repetitions", false)
	if status, _ := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Plain training",
	}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the plain training, got %d", status)
	}

	titles := func(query string) []string {
		resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/trainings"+query, nil, token))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		var list []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&list)
		found := make([]string, 0, len(list))
		for _, item := range list {
			found = append(found, item["title"].(string))
		}
		return found
	}

	if got := titles(""); len(got) != 2 {
		t.Errorf("Expected the whole library without the filter, got %v", got)
	}
	if got := titles("?is_assessment=true"); len(got) != 1 || got[0] != "Pull up pyramid" {
		t.Errorf("Expected only the assessment, got %v", got)
	}
	if got := titles("?is_assessment=false"); len(got) != 1 || got[0] != "Plain training" {
		t.Errorf("Expected only the plain training, got %v", got)
	}

	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/trainings?is_assessment=maybe", nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected %d for an unreadable filter, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

// The editor asks whether the unit and the hands are still free before offering
// them, so the flag has to follow both reasons they lock.
func TestAssessmentDefinitions_ReportsWhenTheUnitIsLocked(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assesslock@test.com")
	app := assessmentApp(t, pool, queries)

	trainingID, assessmentID := createAssessmentTraining(t, app, token, "Pyramid", "repetitions", false)

	lockedFor := func(id string) bool {
		resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/assessment-definitions", nil, token))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		var list []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&list)
		for _, d := range list {
			if d["id"] == id {
				return d["unit_locked"] == true
			}
		}
		t.Fatalf("Expected %s in the list", id)
		return false
	}

	if lockedFor(assessmentID) {
		t.Errorf("Expected a fresh assessment to be free to re-unit")
	}

	// A training reading a number against it locks the unit.
	status, _ := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
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
		t.Fatalf("Expected 201, got %d", status)
	}

	if !lockedFor(assessmentID) {
		t.Errorf("Expected a referenced assessment to report its unit locked")
	}

	// The training the assessment is run from carries the same answer.
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/trainings/"+trainingID, nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var training map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&training)
	definition := training["assessment"].(map[string]interface{})
	if definition["unit_locked"] != true {
		t.Errorf("Expected the training's assessment to report its unit locked, got %v", definition)
	}
	if definition["training_id"] != trainingID {
		t.Errorf("Expected the assessment to name the training it is run from, got %v", definition["training_id"])
	}
}
