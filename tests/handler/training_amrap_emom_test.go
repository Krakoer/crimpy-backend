package handler_test

import (
	"crimpy/backend/tests/testutil"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// emomTestApp builds an app that can create trainings and declare assessments,
// which is what a reps target needs to be reachable at all.
func emomTestApp(t *testing.T, email string) (*fiber.App, string) {
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
	app, token := emomTestApp(t, "emom1@test.com")

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
	app, token := emomTestApp(t, "emom2@test.com")

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
	app, token := emomTestApp(t, "emom3@test.com")

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
			app, token := emomTestApp(t, "emomrest"+field+"@test.com")

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
	app, token := emomTestApp(t, "amrap1@test.com")

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
	app, token := emomTestApp(t, "amrap2@test.com")

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
	app, token := emomTestApp(t, "amrap3@test.com")

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
