package handler_test

import (
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// The four prescriptions the ticket is measured against, written the way a
// coach writes them. They are the acceptance test of the whole column: if one
// of them cannot be stored and read back word for word, free text was the wrong
// shape for it.
var workedExampleProtocols = map[string]string{
	"branch on the result": "Hang on 20mm, 3 fingers extended, to failure or 40s, 3 sets, 2 min rest. If you go past 40s add 5kg; if you fall short, put your feet on the ground.",
	"derive later sets":    "5 on / 5 off on 20mm. Max reps on set 1, but stop at 36 if you have not failed by then. Then minus 25% of that, rounded down, on each following set.",
	"ramp to a limit":      "Ramp up in 5kg steps, then 2.5kg once you are near the limit. If you hold more than 5 sec, stop the set and add load.",
	"stop rule on a test":  "Aim for 24 reps. If you get past 24, stop, rest 10 min, then add 5kg and go again.",
}

func TestTrainingItemProtocol_RejectsOverLength(t *testing.T) {
	longProtocol := strings.Repeat("a", 2001)
	status, result := createTrainingWithItems(t, "itemprotocolover@test.com", []map[string]interface{}{
		{"type": "exercise", "protocol": longProtocol},
	})

	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a protocol over the limit, got %d: %v", status, result)
	}
	if !strings.Contains(result["error"].(string), "2000") {
		t.Errorf("Expected the refusal to name the limit, got %v", result["error"])
	}
}

// Counted in runes, so a rule written in the accented French the spreadsheet
// uses gets the same 2000 characters as one written in ASCII, and the boundary
// itself is accepted rather than refused.
func TestTrainingItemProtocol_AllowsExactlyTheLimitInMultibyteRunes(t *testing.T) {
	protocol := strings.Repeat("e", 2000)
	status, result := createTrainingWithItems(t, "itemprotocolexactlimit@test.com", []map[string]interface{}{
		{"type": "exercise", "protocol": protocol},
	})

	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 for a protocol at exactly the limit, got %d: %v", status, result)
	}
	item := firstItem(t, result)
	if item["protocol"] != protocol {
		t.Errorf("Expected the protocol to round trip in full, got %v", item["protocol"])
	}
}

// The acceptance test of the ticket. Each of the four prescriptions the issue
// lists has to survive the write and the read unchanged, punctuation and
// percent signs included, since the athlete resolves it by reading it.
func TestTrainingItemProtocol_CarriesEveryWorkedExample(t *testing.T) {
	for name, protocol := range workedExampleProtocols {
		t.Run(name, func(t *testing.T) {
			slug := strings.ReplaceAll(name, " ", "")
			status, result := createTrainingWithItems(t, "itemprotocol"+slug+"@test.com", []map[string]interface{}{
				{"type": "exercise", "protocol": protocol},
			})

			if status != fiber.StatusCreated {
				t.Fatalf("Expected 201 for %q, got %d: %v", name, status, result)
			}
			if got := firstItem(t, result)["protocol"]; got != protocol {
				t.Errorf("Expected %q to round trip, got %v", name, got)
			}
		})
	}
}

// A rule the coach broke over several lines is stored as they wrote it: the
// line breaks are how a protocol with a branch in it is read.
func TestTrainingItemProtocol_KeepsTheLineBreaksTheCoachWrote(t *testing.T) {
	protocol := "Set 1: max reps, stop at 36.\nSets 2 and 3: minus 25%, rounded down."
	status, result := createTrainingWithItems(t, "itemprotocollines@test.com", []map[string]interface{}{
		{"type": "exercise", "protocol": protocol},
	})

	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, result)
	}
	if got := firstItem(t, result)["protocol"]; got != protocol {
		t.Errorf("Expected the line breaks to survive, got %q", got)
	}
}

// The column is type agnostic, like the comment and the goal beside it: a
// circuit is as much a thing a coach puts a stop rule on as an exercise is.
func TestTrainingItemProtocol_RoundTripsOnEveryItemType(t *testing.T) {
	const protocol = "Stop the set once a rep slows down."
	for _, itemType := range []string{"exercise", "repeater", "hangboard_rep", "circuit", "group", "emom", "free"} {
		t.Run(itemType, func(t *testing.T) {
			item := map[string]interface{}{"type": itemType, "protocol": protocol}
			if itemType == "emom" {
				item["interval_seconds"] = 60
			}
			status, result := createTrainingWithItems(t, "itemprotocol-"+itemType+"@test.com", []map[string]interface{}{item})

			if status != fiber.StatusCreated {
				t.Fatalf("Expected 201 for a %s carrying a protocol, got %d: %v", itemType, status, result)
			}
			if got := firstItem(t, result)["protocol"]; got != protocol {
				t.Errorf("Expected the protocol to round trip on a %s, got %v", itemType, got)
			}
		})
	}
}

// What it has to survive is the edit path, which rewrites every column of an
// item sent back under its own id.
func TestTrainingItemProtocol_SurvivesAnUpdateAndIsReadBack(t *testing.T) {
	app, token := openItemsApp(t, "itemprotocolupdate@test.com")

	protocol := workedExampleProtocols["branch on the result"]
	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Hang block",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps": 3, "protocol": protocol},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}
	itemID := firstItem(t, training)["id"].(string)

	status, updated := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Hang block",
		"items": []map[string]interface{}{
			{"id": itemID, "type": "exercise", "reps": 4, "protocol": protocol},
		},
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 updating the training, got %d: %v", status, updated)
	}
	if got := firstItem(t, updated)["protocol"]; got != protocol {
		t.Errorf("Expected the protocol to survive the update, got %v", got)
	}

	stored := firstItem(t, getTraining(t, app, token, training["id"].(string)))
	if stored["protocol"] != protocol {
		t.Errorf("Expected the stored protocol to be read back, got %v", stored["protocol"])
	}
}

// A protocol the coach dropped has to leave, rather than being kept by the
// omission that means "unchanged" in no other field of this payload.
func TestTrainingItemProtocol_ClearedByAnUpdateThatOmitsIt(t *testing.T) {
	app, token := openItemsApp(t, "itemprotocolclear@test.com")

	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Test block",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps": 5, "protocol": workedExampleProtocols["stop rule on a test"]},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}
	itemID := firstItem(t, training)["id"].(string)

	status, updated := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Test block",
		"items": []map[string]interface{}{
			{"id": itemID, "type": "exercise", "reps": 5},
		},
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 updating the training, got %d: %v", status, updated)
	}
	if got, ok := firstItem(t, updated)["protocol"]; ok {
		t.Errorf("Expected the protocol to be gone, got %v", got)
	}
}

// Three free text columns now sit on an item and they answer three different
// questions: what the block is for, how to execute the movement, and the rule
// that decides what the numbers become. Writing one must not disturb the others.
func TestTrainingItemProtocol_IsHeldApartFromTheCommentAndTheGoal(t *testing.T) {
	status, result := createTrainingWithItems(t, "itemprotocolvsothers@test.com", []map[string]interface{}{
		{
			"type":     "exercise",
			"goal":     "resi doigts",
			"comment":  "first rep in pronation",
			"protocol": workedExampleProtocols["ramp to a limit"],
		},
	})

	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, result)
	}
	item := firstItem(t, result)
	if item["goal"] != "resi doigts" {
		t.Errorf("Expected the goal to round trip, got %v", item["goal"])
	}
	if item["comment"] != "first rep in pronation" {
		t.Errorf("Expected the comment to round trip, got %v", item["comment"])
	}
	if item["protocol"] != workedExampleProtocols["ramp to a limit"] {
		t.Errorf("Expected the protocol to round trip, got %v", item["protocol"])
	}
}

// The payoff of the ticket is the athlete reading the rule while they resolve
// it, and what they read is the frozen prescription, not the training: the
// training stays editable behind it. So the protocol has to survive into the
// snapshot.
//
// The same test pins the decision that protocol is not an override key, on the
// one read where being wrong about it would show: a week that sends one is
// accepted and ignored, so the block keeps the protocol the training gave it
// while its numbers are retuned around it.
func TestTrainingItemProtocol_ReachesTheFrozenPrescription(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "protocolpresc")
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	protocol := workedExampleProtocols["derive later sets"]
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
			"protocol":            protocol,
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
						"overrides": map[string]interface{}{"reps": 5, "protocol": "stop at 20"},
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
	if item["protocol"] != protocol {
		t.Errorf("Expected the training's protocol in the snapshot, got %v", item["protocol"])
	}
	if item["reps"] != float64(5) {
		t.Errorf("Expected the override to retune the reps to 5, got %v", item["reps"])
	}
}
