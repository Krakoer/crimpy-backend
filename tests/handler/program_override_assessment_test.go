package handler_test

import (
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// myProgramTraining is the athlete read of a training their program schedules.
func myProgramTraining(t *testing.T, app *fiber.App, userToken, programID, trainingID string) map[string]interface{} {
	t.Helper()
	url := fmt.Sprintf("/api/user/programs/%s/trainings/%s", programID, trainingID)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, url, nil, userToken))
	if err != nil {
		t.Fatalf("Request to %s failed: %v", url, err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 reading the program training, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

// namedAssessments keys the definitions a training read carries by assessment id.
func namedAssessments(training map[string]interface{}) map[string]map[string]interface{} {
	named := map[string]map[string]interface{}{}
	referenced, _ := training["referenced_assessments"].([]interface{})
	for _, entry := range referenced {
		definition := entry.(map[string]interface{})
		named[definition["id"].(string)] = definition
	}
	return named
}

// createPlainTraining makes a training whose single item references no
// assessment, which is the case a week override has to name on its own.
func createPlainTraining(t *testing.T, app *fiber.App, coachToken, title string) (trainingID, itemID string) {
	t.Helper()
	status, training := postJSON(t, app, "/api/trainings", coachToken, map[string]interface{}{
		"title": title,
		"items": []map[string]interface{}{{"type": "exercise", "reps": 10}},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating %q, got %d: %v", title, status, training)
	}
	item := training["items"].([]interface{})[0].(map[string]interface{})
	return training["id"].(string), item["id"].(string)
}

// scheduleWithOverride puts the training on a week of the program with one
// override on the given item.
func scheduleWithOverride(t *testing.T, app *fiber.App, coachToken, userID, programID, trainingID, itemID string, weekNum int, overrides map[string]interface{}) {
	t.Helper()
	upsertWeekSessions(t, app, coachToken, userID, programID, weekNum, map[string]interface{}{
		"sessions": []map[string]interface{}{{
			"training_id": trainingID,
			"day_of_week": 0,
			"overrides": []map[string]interface{}{{
				"item_id":   itemID,
				"overrides": overrides,
			}},
		}},
	})
}

// An override may prescribe a percentage of an assessment the training itself
// never references, which is exactly when its name matters most: the athlete
// reads a percentage of something, and the number in play is the fallback. The
// definition reaches them nowhere else, since the training freezes its
// referenced assessments from its own items and a coach's assessment is not in
// the catalog an athlete can fetch.
func TestProgramTraining_NamesAnAssessmentOnlyAWeekOverrideReferences(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "overrideassess1")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Max pull ups", "repetitions", false)
	trainingID, itemID := createPlainTraining(t, app, coachToken, "Pull day")

	scheduleWithOverride(t, app, coachToken, userID, programID, trainingID, itemID, 1, map[string]interface{}{
		"variable_targets": map[string]interface{}{
			"reps": map[string]interface{}{
				"assessment_id": assessmentID, "percent": 75, "fallback": 10,
			},
		},
	})

	training := myProgramTraining(t, app, userToken, programID, trainingID)

	item := training["items"].([]interface{})[0].(map[string]interface{})
	if targets, present := item["variable_targets"]; present && targets != nil {
		t.Fatalf("Expected the training item itself to reference no assessment, got %v", targets)
	}

	named := namedAssessments(training)
	definition, ok := named[assessmentID]
	if !ok {
		t.Fatalf("Expected the override's assessment named, got %v", training["referenced_assessments"])
	}
	if definition["label"] != "Max pull ups" || definition["unit"] != "repetitions" {
		t.Errorf("Expected the label and unit of the override's assessment, got %v", definition)
	}
}

// A load expressed as a percentage of an assessment names one the same way a
// variable target does, so an override carrying one has to be named too.
func TestProgramTraining_NamesAnAssessmentAnOverrideLoadReferences(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "overrideassess2")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Max hang", "kilograms", false)
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	scheduleWithOverride(t, app, coachToken, userID, programID, trainingID, itemID, 1, map[string]interface{}{
		"loads": []map[string]interface{}{
			{"unit": "percent_assessment", "assessment_id": assessmentID, "value": 80, "fallback": 20},
		},
	})

	named := namedAssessments(myProgramTraining(t, app, userToken, programID, trainingID))
	if _, ok := named[assessmentID]; !ok {
		t.Fatalf("Expected the assessment the override load reads against named, got %v", named)
	}
}

// The definitions an athlete is handed are the ones they are prescribed
// against, so an assessment their coach owns but only prescribes on another
// training, or does not prescribe at all, stays unnamed.
func TestProgramTraining_LeavesOutAnAssessmentItIsNotPrescribedAgainst(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "overrideassess3")

	_, prescribedID := createAssessmentTraining(t, app, coachToken, "Max pull ups", "repetitions", false)
	_, elsewhereID := createAssessmentTraining(t, app, coachToken, "Max push ups", "repetitions", false)
	_, unusedID := createAssessmentTraining(t, app, coachToken, "Max dips", "repetitions", false)

	trainingID, itemID := createPlainTraining(t, app, coachToken, "Pull day")
	otherTrainingID, otherItemID := createPlainTraining(t, app, coachToken, "Push day")

	target := func(assessmentID string) map[string]interface{} {
		return map[string]interface{}{
			"variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{
					"assessment_id": assessmentID, "percent": 75, "fallback": 10,
				},
			},
		}
	}
	scheduleWithOverride(t, app, coachToken, userID, programID, trainingID, itemID, 1, target(prescribedID))
	scheduleWithOverride(t, app, coachToken, userID, programID, otherTrainingID, otherItemID, 2, target(elsewhereID))

	named := namedAssessments(myProgramTraining(t, app, userToken, programID, trainingID))
	if _, ok := named[prescribedID]; !ok {
		t.Fatalf("Expected the assessment this training is prescribed against named, got %v", named)
	}
	if _, ok := named[elsewhereID]; ok {
		t.Errorf("Expected no definition for an assessment only another training is prescribed against, got %v", named)
	}
	if _, ok := named[unusedID]; ok {
		t.Errorf("Expected no definition for an assessment nothing prescribes, got %v", named)
	}
}
