package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// openItemsApp builds an app that can create trainings, play sessions and
// declare assessments, which is what a reps target needs to be reachable at all.
func openItemsApp(t *testing.T, email string) (*fiber.App, string) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	_, token := testutil.CreateTestUser(t, queries, email)
	return assessmentApp(t, pool, queries), token
}

func firstItem(t *testing.T, training map[string]interface{}) map[string]interface{} {
	t.Helper()
	items, ok := training["items"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("Expected the training to carry items, got %v", training["items"])
	}
	return items[0].(map[string]interface{})
}

func TestTrainingEmom_RoundTripsIntervalAndChildren(t *testing.T) {
	app, token := openItemsApp(t, "emom1@test.com")

	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Pull up EMOM",
		"items": []map[string]interface{}{
			{
				"type":             "emom",
				"cycles":           10,
				"interval_seconds": 60,
				"group_title":      "Pull ups every minute",
				"items": []map[string]interface{}{
					{"type": "exercise", "reps": 5},
				},
			},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating an emom, got %d: %v", status, training)
	}

	item := firstItem(t, training)
	if item["type"] != "emom" {
		t.Errorf("Expected the item type emom, got %v", item["type"])
	}
	if item["interval_seconds"] != float64(60) {
		t.Errorf("Expected the interval to round-trip as 60, got %v", item["interval_seconds"])
	}
	if item["cycles"] != float64(10) {
		t.Errorf("Expected 10 rounds, got %v", item["cycles"])
	}
	children, ok := item["items"].([]interface{})
	if !ok || len(children) != 1 {
		t.Fatalf("Expected the emom to hold its one child, got %v", item["items"])
	}
	if children[0].(map[string]interface{})["reps"] != float64(5) {
		t.Errorf("Expected the child to keep its 5 reps, got %v", children[0])
	}
}

func TestTrainingEmom_RejectsEmomWithoutInterval(t *testing.T) {
	app, token := openItemsApp(t, "emom2@test.com")

	status, result := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Clockless EMOM",
		"items": []map[string]interface{}{
			{"type": "emom", "cycles": 10},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for an emom with no interval, got %d: %v", status, result)
	}
}

func TestTrainingEmom_RejectsIntervalOnAnotherType(t *testing.T) {
	app, token := openItemsApp(t, "emom3@test.com")

	status, result := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Circuit on a clock",
		"items": []map[string]interface{}{
			{"type": "circuit", "cycles": 3, "interval_seconds": 60},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a circuit carrying an interval, got %d: %v", status, result)
	}
}

// An emom rests for whatever is left of its interval, so a rest stored on it
// names a gap the block never plays.
func TestTrainingEmom_RejectsRestFields(t *testing.T) {
	for _, field := range []string{"cycle_rest_seconds", "rest_seconds"} {
		t.Run(field, func(t *testing.T) {
			app, token := openItemsApp(t, "emomrest"+field+"@test.com")

			status, result := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
				"title": "EMOM with a rest",
				"items": []map[string]interface{}{
					{"type": "emom", "cycles": 10, "interval_seconds": 60, field: 30},
				},
			})
			if status != fiber.StatusBadRequest {
				t.Fatalf("Expected 400 for an emom carrying %s, got %d: %v", field, status, result)
			}
		})
	}
}

func TestTrainingAmrap_RoundTripsRepsIsMaxOnExercise(t *testing.T) {
	app, token := openItemsApp(t, "amrap1@test.com")

	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Max pull ups",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps_is_max": true, "rest_seconds": 120},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating an AMRAP exercise, got %d: %v", status, training)
	}

	if item := firstItem(t, training); item["reps_is_max"] != true {
		t.Errorf("Expected reps_is_max to round-trip, got %v", item["reps_is_max"])
	}
}

// A repeater lays its loads, grips and edges out one row per rep, so it has no
// layout to be written against once the rep count is left open.
func TestTrainingAmrap_RejectsRepsIsMaxOnRepeater(t *testing.T) {
	app, token := openItemsApp(t, "amrap2@test.com")

	status, result := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Open repeater",
		"items": []map[string]interface{}{
			{"type": "repeater", "cycles": 2, "reps": 6, "reps_is_max": true, "hand": "both", "granularity": "uniform"},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for reps_is_max on a repeater, got %d: %v", status, result)
	}
}

func TestTrainingAmrap_RejectsRepsIsMaxWithRepsTarget(t *testing.T) {
	app, token := openItemsApp(t, "amrap3@test.com")

	_, assessmentID := createAssessmentTraining(t, app, token, "Pull up max", "repetitions", false)

	status, result := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Open and prescribed at once",
		"items": []map[string]interface{}{
			{
				"type":        "exercise",
				"reps":        8,
				"reps_is_max": true,
				"variable_targets": map[string]interface{}{
					"reps": map[string]interface{}{
						"assessment_id": assessmentID, "percent": 60, "fallback": 8,
					},
				},
			},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for an open rep count that is also a percentage, got %d: %v", status, result)
	}
}

// A program week override reaches the item columns without going through the
// training write path, so the invariants that path enforces have to hold here
// too. Otherwise a coach prescribes a week that says both "the rep count is
// open" and "the rep count is a percentage of your max", which no client can
// run and the direct write path refuses outright.
func TestTrainingAmrap_RejectsOverrideReachingAForbiddenShape(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "amrapover@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "amrapoveruser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler:             handler.NewTrainingHandler(queries, pool),
		ProgramHandler:              handler.NewProgramHandler(queries, pool),
		AssessmentDefinitionHandler: handler.NewAssessmentDefinitionHandler(queries, pool),
		AssessmentHandler:           handler.NewAssessmentHandler(queries),
		SessionHandler:              handler.NewSessionHandler(queries, pool),
	})
	programID := createTestProgram(t, coachToken, userID, app)

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)

	status, training := postJSON(t, app, "/api/trainings", coachToken, map[string]interface{}{
		"title": "Open blocks",
		"items": []map[string]interface{}{
			{"type": "emom", "cycles": 10, "interval_seconds": 60, "items": []map[string]interface{}{
				{"type": "exercise", "reps_is_max": true},
			}},
			{
				"type": "repeater", "cycles": 1, "reps": 6, "worktime_seconds": 7,
				"rest_seconds": 3, "hand": "both", "granularity": "uniform",
				"loads": []map[string]interface{}{{"value": 0, "unit": "bw"}},
			},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}
	trainingID := training["id"].(string)
	emom := firstItem(t, training)
	emomID := emom["id"].(string)
	exerciseID := emom["items"].([]interface{})[0].(map[string]interface{})["id"].(string)
	repeaterID := training["items"].([]interface{})[1].(map[string]interface{})["id"].(string)

	for name, override := range map[string]map[string]interface{}{
		"a rest on an emom": {"item_id": emomID, "overrides": map[string]interface{}{"rest_seconds": 45}},
		"a cycle rest on an emom": {
			"item_id": emomID, "overrides": map[string]interface{}{"cycle_rest_seconds": 45},
		},
		"a percentage on an open rep count": {
			"item_id": exerciseID,
			"overrides": map[string]interface{}{
				"variable_targets": map[string]interface{}{
					"reps": map[string]interface{}{
						"assessment_id": assessmentID, "percent": 60, "fallback": 8,
					},
				},
			},
		},
		"a clock on an exercise": {
			"item_id": exerciseID, "overrides": map[string]interface{}{"interval_seconds": 60},
		},
		"an open rep count on a repeater": {
			"item_id": repeaterID, "overrides": map[string]interface{}{"reps_is_max": true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := upsertWeekStatus(t, app, coachToken, userID, programID, 1, map[string]interface{}{
				"sessions": []map[string]interface{}{
					{
						"training_id": trainingID,
						"day_of_week": 0,
						"overrides":   []map[string]interface{}{override},
					},
				},
			})
			if got != fiber.StatusBadRequest {
				t.Fatalf("Expected 400 for %s through an override, got %d", name, got)
			}
		})
	}
}
