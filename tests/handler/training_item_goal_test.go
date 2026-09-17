package handler_test

import (
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestTrainingItemGoal_RejectsOverLength(t *testing.T) {
	longGoal := strings.Repeat("a", 201)
	status, result := createTrainingWithItems(t, "itemgoalover@test.com", []map[string]interface{}{
		{"type": "exercise", "goal": longGoal},
	})

	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a goal over the limit, got %d: %v", status, result)
	}
	if !strings.Contains(result["error"].(string), "200") {
		t.Errorf("Expected the refusal to name the limit, got %v", result["error"])
	}
}

// The limit is counted in runes, not bytes, so a goal written in the accented
// French the spreadsheet uses gets the same 200 characters as one written in
// ASCII, and the boundary itself must be accepted rather than refused.
func TestTrainingItemGoal_AllowsExactlyTheLimitInMultibyteRunes(t *testing.T) {
	goal := strings.Repeat("é", 200)
	status, result := createTrainingWithItems(t, "itemgoalexactlimit@test.com", []map[string]interface{}{
		{"type": "exercise", "goal": goal},
	})

	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 for a goal at exactly the limit, got %d: %v", status, result)
	}
	item := firstItem(t, result)
	if item["goal"] != goal {
		t.Errorf("Expected the goal to round trip in full, got %v", item["goal"])
	}
}

// The column is type agnostic, like the comment beside it: a block of pulling
// work is as much a thing the program has a reason for as an exercise is.
func TestTrainingItemGoal_RoundTripsOnEveryItemType(t *testing.T) {
	for _, itemType := range []string{"exercise", "repeater", "hangboard_rep", "circuit", "group", "emom", "free"} {
		t.Run(itemType, func(t *testing.T) {
			item := map[string]interface{}{"type": itemType, "goal": "resi doigts"}
			if itemType == "emom" {
				item["interval_seconds"] = 60
			}
			status, result := createTrainingWithItems(t, "itemgoal-"+itemType+"@test.com", []map[string]interface{}{item})

			if status != fiber.StatusCreated {
				t.Fatalf("Expected 201 for a %s carrying a goal, got %d: %v", itemType, status, result)
			}
			got := firstItem(t, result)
			if got["goal"] != "resi doigts" {
				t.Errorf("Expected the goal to round trip on a %s, got %v", itemType, got["goal"])
			}
		})
	}
}

// The goal is why the exercise is in the program, so it is stable across the
// weeks that retune it. What it has to survive is the edit path, which rewrites
// every column of an item sent back under its own id.
func TestTrainingItemGoal_SurvivesAnUpdateAndIsReadBack(t *testing.T) {
	app, token := openItemsApp(t, "itemgoalupdate@test.com")

	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Pulling block",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps": 5, "goal": "force/hypertrophie des muscles de poussee"},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}
	itemID := firstItem(t, training)["id"].(string)

	status, updated := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Pulling block",
		"items": []map[string]interface{}{
			{"id": itemID, "type": "exercise", "reps": 8, "goal": "force/hypertrophie des muscles de poussee"},
		},
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 updating the training, got %d: %v", status, updated)
	}
	if got := firstItem(t, updated)["goal"]; got != "force/hypertrophie des muscles de poussee" {
		t.Errorf("Expected the goal to survive the update, got %v", got)
	}

	stored := firstItem(t, getTraining(t, app, token, training["id"].(string)))
	if stored["goal"] != "force/hypertrophie des muscles de poussee" {
		t.Errorf("Expected the stored goal to be read back, got %v", stored["goal"])
	}
}

// A goal cleared by the coach has to leave, rather than being kept by the
// omission that means "unchanged" in no other field of this payload.
func TestTrainingItemGoal_ClearedByAnUpdateThatOmitsIt(t *testing.T) {
	app, token := openItemsApp(t, "itemgoalclear@test.com")

	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Leg block",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps": 5, "goal": "explo jambes"},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}
	itemID := firstItem(t, training)["id"].(string)

	status, updated := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Leg block",
		"items": []map[string]interface{}{
			{"id": itemID, "type": "exercise", "reps": 5},
		},
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 updating the training, got %d: %v", status, updated)
	}
	if got, ok := firstItem(t, updated)["goal"]; ok {
		t.Errorf("Expected the goal to be gone, got %v", got)
	}
}

// Goal and comment answer different questions and are stored in different
// columns, which the spreadsheet this came from kept apart too. Writing one
// must not disturb the other.
func TestTrainingItemGoal_IsHeldApartFromTheComment(t *testing.T) {
	status, result := createTrainingWithItems(t, "itemgoalvscomment@test.com", []map[string]interface{}{
		{"type": "exercise", "goal": "capacite/endurance doigts", "comment": "first rep in pronation"},
	})

	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, result)
	}
	item := firstItem(t, result)
	if item["goal"] != "capacite/endurance doigts" {
		t.Errorf("Expected the goal to round trip, got %v", item["goal"])
	}
	if item["comment"] != "first rep in pronation" {
		t.Errorf("Expected the comment to round trip, got %v", item["comment"])
	}
}

// The payoff of the whole ticket is the athlete reading why the block is in
// their program, and what they read is the frozen prescription, not the
// training: the training stays editable behind it. So the goal has to survive
// into the snapshot.
//
// The same test pins the decision that goal is not an override key, on the one
// read where being wrong about it would show: a week that sends one is accepted
// and ignored, so the block keeps the goal the training gave it while its
// numbers are retuned around it.
func TestTrainingItemGoal_ReachesTheFrozenPrescription(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "goalpresc")
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	replaceTrainingItems(t, app, coachToken, trainingID, []map[string]interface{}{
		{
			"id":                  itemID,
			"type":                "repeater",
			"reps":                6,
			"hb_worktime_seconds": 7,
			"rest_seconds":        60,
			"hand":                "both",
			"granularity":         "uniform",
			"loads":               []map[string]interface{}{{"value": 0, "unit": "bw"}},
			"goal":                "resi doigts",
		},
	})

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{
						"item_id":   itemID,
						"overrides": map[string]interface{}{"reps": 5, "goal": "explo jambes"},
					},
				},
			},
		},
	})

	session := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": weekSessionIDs(created)[0],
	})

	items := prescriptionItems(t, sessionPrescription(t, session))
	if len(items) != 1 {
		t.Fatalf("Expected 1 prescription item, got %d", len(items))
	}
	item := items[0].(map[string]interface{})
	if item["goal"] != "resi doigts" {
		t.Errorf("Expected the training's goal in the snapshot, got %v", item["goal"])
	}
	if item["reps"] != float64(5) {
		t.Errorf("Expected the override to retune the reps to 5, got %v", item["reps"])
	}
}
