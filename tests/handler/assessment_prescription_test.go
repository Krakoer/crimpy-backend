package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// setupAssessmentProgramApp is setupFrozenSessionApp with the assessment
// handlers wired, so a coach can write an assessment and prescribe a training
// that references it.
func setupAssessmentProgramApp(t *testing.T, prefix string) (app *fiber.App, coachToken, userToken, userID, programID string) {
	t.Helper()
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, prefix+"coach@test.com")
	userID, userToken = testutil.CreateTestUser(t, queries, prefix+"user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app = testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:              handler.NewSessionHandler(queries, pool),
		TrainingHandler:             handler.NewTrainingHandler(queries, pool),
		ProgramHandler:              handler.NewProgramHandler(queries, pool),
		AssessmentHandler:           handler.NewAssessmentHandler(queries),
		AssessmentDefinitionHandler: handler.NewAssessmentDefinitionHandler(queries, pool),
	})
	programID = createTestProgram(t, coachToken, userID, app)
	return app, coachToken, userToken, userID, programID
}

// A prescription that references a custom assessment has to name it, or the
// athlete's client cannot label the percentage nor check it drives the right
// field, and the coach could rename the assessment and restate what a past
// session asked for.
func TestSessionPrescription_FreezesCustomAssessmentDefinition(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupAssessmentProgramApp(t, "prescustom")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "One arm lock off", "seconds", true)

	status, training := postJSON(t, app, "/api/trainings", coachToken, map[string]interface{}{
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
		t.Fatalf("Expected 201 creating the referencing training, got %d: %v", status, training)
	}
	trainingID := training["id"].(string)

	programSessionID := prescribeSession(t, app, coachToken, userID, programID, trainingID, nil)
	created := playSession(t, app, userToken, map[string]interface{}{
		"program_session_id": programSessionID,
	})

	inputs := sessionPrescription(t, created)["resolved_against"].(map[string]interface{})
	definitions, ok := inputs["definitions"].([]interface{})
	if !ok || len(definitions) != 1 {
		t.Fatalf("Expected the referenced assessment frozen with the prescription, got %v", inputs["definitions"])
	}
	frozen := definitions[0].(map[string]interface{})
	if frozen["id"] != assessmentID {
		t.Errorf("Expected the assessment named, got %v", frozen["id"])
	}
	if frozen["label"] != "One arm lock off" || frozen["unit"] != "seconds" || frozen["per_hand"] != true {
		t.Errorf("Expected the label, unit and hands frozen, got %v", frozen)
	}

	// The coach renames it. What the athlete was asked for does not change.
	body, _ := json.Marshal(map[string]interface{}{
		"label": "One arm lock off, 90 degrees", "prompt": "How long?", "unit": "seconds", "per_hand": true,
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/assessment-definitions/"+assessmentID, body, coachToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected the rename to be allowed, got %d", resp.StatusCode)
	}

	reread := sessionPrescription(t, getSessionJSON(t, app, userToken, created["id"].(string)))
	rereadInputs := reread["resolved_against"].(map[string]interface{})
	stillFrozen := rereadInputs["definitions"].([]interface{})[0].(map[string]interface{})
	if stillFrozen["label"] != "One arm lock off" {
		t.Errorf("Expected the frozen label to survive the rename, got %v", stillFrozen["label"])
	}
}

// An override replaces the loads and the targets of an item wholesale, so the
// references it introduces are the ones that have to be checked. Resolving the
// base training's references instead would let a coach name an assessment that
// is not theirs.
func TestSessionPrescription_RejectsForeignAssessmentInOverride(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "ovrcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "ovruser@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	_, strangerToken := testutil.CreateTestUser(t, queries, "ovrstranger@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:              handler.NewSessionHandler(queries, pool),
		TrainingHandler:             handler.NewTrainingHandler(queries, pool),
		ProgramHandler:              handler.NewProgramHandler(queries, pool),
		AssessmentHandler:           handler.NewAssessmentHandler(queries),
		AssessmentDefinitionHandler: handler.NewAssessmentDefinitionHandler(queries, pool),
	})
	programID := createTestProgram(t, coachToken, userID, app)

	// The stranger's assessment, which the coach has no business referencing.
	_, foreignID := createAssessmentTraining(t, app, strangerToken, "Not the coach's", "seconds", false)

	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{
						"item_id": itemID,
						"overrides": map[string]interface{}{
							"variable_targets": map[string]interface{}{
								"duration": map[string]interface{}{
									"assessment_id": foreignID, "percent": 50, "fallback": 10,
								},
							},
						},
					},
				},
			},
		},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected %d for an override naming another user's assessment, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

// The same override, naming the coach's own assessment, is allowed: the check is
// ownership, not a ban on overriding a reference.
func TestSessionPrescription_AcceptsOwnAssessmentInOverride(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, userID, programID := setupAssessmentProgramApp(t, "ovrown")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Coach's own test", "seconds", false)
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{
						"item_id": itemID,
						"overrides": map[string]interface{}{
							"variable_targets": map[string]interface{}{
								"duration": map[string]interface{}{
									"assessment_id": assessmentID, "percent": 50, "fallback": 10,
								},
							},
						},
					},
				},
			},
		},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected the coach's own assessment to be accepted in an override, got %d", resp.StatusCode)
	}
}
