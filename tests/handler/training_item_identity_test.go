package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func putJSON(t *testing.T, app *fiber.App, path, token string, payload map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, path, body, token))
	if err != nil {
		t.Fatalf("Request to %s failed: %v", path, err)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func getTraining(t *testing.T, app *fiber.App, token, trainingID string) map[string]interface{} {
	t.Helper()
	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, "/api/trainings/"+trainingID, nil, token))
	if err != nil {
		t.Fatalf("Failed to get training: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 getting the training, got %d", resp.StatusCode)
	}
	var training map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&training)
	return training
}

// treeItemIDs reads the id of every item in a training response, depth first,
// so two trees can be compared node by node.
func treeItemIDs(t *testing.T, training map[string]interface{}) []string {
	t.Helper()
	var walk func(raw interface{}) []string
	walk = func(raw interface{}) []string {
		items, _ := raw.([]interface{})
		ids := make([]string, 0, len(items))
		for _, entry := range items {
			item, ok := entry.(map[string]interface{})
			if !ok {
				t.Fatalf("Expected an item object, got %v", entry)
			}
			id, ok := item["id"].(string)
			if !ok {
				t.Fatalf("Expected the item to carry an id, got %v", item["id"])
			}
			ids = append(ids, id)
			ids = append(ids, walk(item["items"])...)
		}
		return ids
	}
	return walk(training["items"])
}

// createNestedTraining makes a training holding a group with one exercise in it
// followed by a free item, and returns the training with its ids in tree order.
func createNestedTraining(t *testing.T, app *fiber.App, token string) (map[string]interface{}, []string) {
	t.Helper()
	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Board session",
		"items": []map[string]interface{}{
			{
				"type":        "group",
				"group_title": "Warm up",
				"items": []map[string]interface{}{
					{"type": "exercise", "reps": 5},
				},
			},
			{"type": "free", "free_text": "Stretch"},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}
	return training, treeItemIDs(t, training)
}

// An item the coach did not touch has to come back under the id it went in
// with: rep data, open counts and program overrides all key into a training by
// item id, and a save that reissues them strands every one of them.
func TestTrainingUpdate_KeepsIdsOfItemsSentBack(t *testing.T) {
	app, token := openItemsApp(t, "itemid1@test.com")
	training, before := createNestedTraining(t, app, token)

	status, updated := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Board session, harder",
		"items": []map[string]interface{}{
			{
				"id":          before[0],
				"type":        "group",
				"group_title": "Warm up",
				"items": []map[string]interface{}{
					{"id": before[1], "type": "exercise", "reps": 8},
				},
			},
			{"id": before[2], "type": "free", "free_text": "Stretch"},
		},
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 updating the training, got %d: %v", status, updated)
	}

	after := treeItemIDs(t, updated)
	if len(after) != len(before) {
		t.Fatalf("Expected %d items after the update, got %d", len(before), len(after))
	}
	for i, id := range before {
		if after[i] != id {
			t.Errorf("Expected item %d to keep id %s, got %s", i, id, after[i])
		}
	}

	group := firstItem(t, updated)
	child := group["items"].([]interface{})[0].(map[string]interface{})
	if child["reps"] != float64(8) {
		t.Errorf("Expected the edit to land, got reps %v", child["reps"])
	}

	stored := treeItemIDs(t, getTraining(t, app, token, training["id"].(string)))
	for i, id := range before {
		if stored[i] != id {
			t.Errorf("Expected the stored item %d to keep id %s, got %s", i, id, stored[i])
		}
	}
}

// The ids only stop rotating; an item the payload drops still goes, and one it
// sends without an id is still added.
func TestTrainingUpdate_AddsNewItemsAndDropsMissingOnes(t *testing.T) {
	app, token := openItemsApp(t, "itemid2@test.com")
	training, before := createNestedTraining(t, app, token)

	status, updated := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Board session",
		"items": []map[string]interface{}{
			{"id": before[2], "type": "free", "free_text": "Stretch"},
			{"type": "exercise", "reps": 12},
		},
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 updating the training, got %d: %v", status, updated)
	}

	after := treeItemIDs(t, updated)
	if len(after) != 2 {
		t.Fatalf("Expected 2 items after the update, got %d", len(after))
	}
	if after[0] != before[2] {
		t.Errorf("Expected the kept item to hold id %s, got %s", before[2], after[0])
	}
	for _, gone := range []string{before[0], before[1]} {
		if after[1] == gone {
			t.Errorf("Expected the added item to hold a new id, got the dropped %s", gone)
		}
	}

	stored := treeItemIDs(t, getTraining(t, app, token, training["id"].(string)))
	if len(stored) != 2 {
		t.Fatalf("Expected the dropped group and its child to be deleted, got %d items", len(stored))
	}
}

// An item moved out of a group keeps its id, so the reps recorded against it
// stay attached to it. The group it came from goes in the same save, and the
// cascade on parent_id would take the child with it if the move were applied
// after the delete rather than before.
func TestTrainingUpdate_KeepsIdOfItemMovedOutOfADeletedGroup(t *testing.T) {
	app, token := openItemsApp(t, "itemid3@test.com")
	training, before := createNestedTraining(t, app, token)

	status, updated := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Board session",
		"items": []map[string]interface{}{
			{"id": before[1], "type": "exercise", "reps": 5},
			{"id": before[2], "type": "free", "free_text": "Stretch"},
		},
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 updating the training, got %d: %v", status, updated)
	}

	stored := treeItemIDs(t, getTraining(t, app, token, training["id"].(string)))
	if len(stored) != 2 {
		t.Fatalf("Expected 2 items after the group was removed, got %d", len(stored))
	}
	if stored[0] != before[1] || stored[1] != before[2] {
		t.Errorf("Expected the ids %v to survive the move, got %v", before[1:], stored)
	}
}

func TestTrainingUpdate_RefusesAnItemIdFromAnotherTraining(t *testing.T) {
	app, token := openItemsApp(t, "itemid4@test.com")
	training, before := createNestedTraining(t, app, token)
	other, otherItems := createNestedTraining(t, app, token)

	status, result := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Board session",
		"items": []map[string]interface{}{
			{"id": otherItems[2], "type": "free", "free_text": "Stretch"},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for an item id from another training, got %d: %v", status, result)
	}

	stored := treeItemIDs(t, getTraining(t, app, token, training["id"].(string)))
	if len(stored) != len(before) {
		t.Errorf("Expected the refused update to change nothing, got %d items", len(stored))
	}
	if treeItemIDs(t, getTraining(t, app, token, other["id"].(string)))[2] != otherItems[2] {
		t.Error("Expected the other training to be left alone")
	}
}

// The scoping on the update is what keeps a coach from writing over an item of
// somebody else's training by naming its id, so it is asserted across two users
// and not only across two trainings of one.
func TestTrainingUpdate_RefusesAnItemIdFromAnotherUsersTraining(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, mine := testutil.CreateTestUser(t, queries, "itemid10mine@test.com")
	_, theirs := testutil.CreateTestUser(t, queries, "itemid10theirs@test.com")
	app := assessmentApp(t, pool, queries)

	training, before := createNestedTraining(t, app, mine)
	other, otherItems := createNestedTraining(t, app, theirs)

	status, result := putJSON(t, app, "/api/trainings/"+training["id"].(string), mine, map[string]interface{}{
		"title": "Board session",
		"items": []map[string]interface{}{
			{"id": otherItems[2], "type": "free", "free_text": "Stretch"},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for an item id owned by another user, got %d: %v", status, result)
	}

	if len(treeItemIDs(t, getTraining(t, app, mine, training["id"].(string)))) != len(before) {
		t.Error("Expected the refused update to change nothing")
	}
	if treeItemIDs(t, getTraining(t, app, theirs, other["id"].(string)))[2] != otherItems[2] {
		t.Error("Expected the other user's training to be left alone")
	}
}

func TestTrainingUpdate_RefusesTheSameItemIdTwice(t *testing.T) {
	app, token := openItemsApp(t, "itemid5@test.com")
	training, before := createNestedTraining(t, app, token)

	status, result := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Board session",
		"items": []map[string]interface{}{
			{"id": before[2], "type": "free", "free_text": "Stretch"},
			{"id": before[2], "type": "free", "free_text": "Stretch again"},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a repeated item id, got %d: %v", status, result)
	}

	if len(treeItemIDs(t, getTraining(t, app, token, training["id"].(string)))) != len(before) {
		t.Error("Expected the refused update to change nothing")
	}
}

func TestTrainingUpdate_RefusesAMalformedItemId(t *testing.T) {
	app, token := openItemsApp(t, "itemid6@test.com")
	training, _ := createNestedTraining(t, app, token)

	status, result := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Board session",
		"items": []map[string]interface{}{
			{"id": "not-a-uuid", "type": "free", "free_text": "Stretch"},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a malformed item id, got %d: %v", status, result)
	}
}

// The open count is the one number in a run that cannot be recomputed, and it is
// keyed by item id against the prescription the session freezes. A coach save
// while the athlete is mid run used to reissue those ids, and the count was
// dropped on arrival with a 201 that said nothing about it.
func TestTrainingUpdate_KeepsOpenCountsRecordedAgainstItsItems(t *testing.T) {
	app, token := openItemsApp(t, "itemid7@test.com")
	trainingID, emomID, exerciseID := createOpenTraining(t, app, token)

	status, updated := putJSON(t, app, "/api/trainings/"+trainingID, token, map[string]interface{}{
		"title": "Pull up EMOM, retitled",
		"items": []map[string]interface{}{
			{
				"id":               emomID,
				"type":             "emom",
				"cycles":           10,
				"interval_seconds": 60,
				"items": []map[string]interface{}{
					{"id": exerciseID, "type": "exercise", "reps_is_max": true},
				},
			},
		},
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 updating the training, got %d: %v", status, updated)
	}

	resp := postSessionWithItemResults(t, app, token, trainingID, []map[string]interface{}{
		{"training_item_id": exerciseID, "occurrence": 0, "field": "reps", "value": 23},
		{"training_item_id": emomID, "occurrence": 0, "field": "cycles", "value": 7},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 playing the session, got %d", resp.StatusCode)
	}
	var session map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&session)

	results := sessionItemResults(t, app, token, session["id"].(string))
	if len(results) != 2 {
		t.Fatalf("Expected the counts recorded before the save to survive it, got %v", results)
	}
}

// A week's overrides are a foreign key on the item they target, cascading on
// delete, so reissuing the ids wiped the overrides of a training the coach
// edited for an unrelated reason.
func TestTrainingUpdate_KeepsProgramOverridesOnItsItems(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "itemid8coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "itemid8user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 8}},
				},
			},
		},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken))
	if err != nil {
		t.Fatalf("Failed to schedule the training: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 scheduling the training, got %d", resp.StatusCode)
	}

	training := getTraining(t, app, coachToken, trainingID)
	items := training["items"].([]interface{})
	payload := make([]map[string]interface{}, 0, len(items))
	for _, entry := range items {
		item := entry.(map[string]interface{})
		payload = append(payload, map[string]interface{}{
			"id":   item["id"],
			"type": item["type"],
			"reps": item["reps"],
		})
	}

	status, updated := putJSON(t, app, "/api/trainings/"+trainingID, coachToken, map[string]interface{}{
		"title": "Renamed by the coach",
		"items": payload,
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 updating the training, got %d: %v", status, updated)
	}

	week := getWeek(t, app, coachToken, userID, programID, 1)
	session := week["sessions"].([]interface{})[0].(map[string]interface{})
	overrides, ok := session["overrides"].([]interface{})
	if !ok || len(overrides) != 1 {
		t.Fatalf("Expected the override to survive the training edit, got %v", session["overrides"])
	}
	if overrides[0].(map[string]interface{})["item_id"] != itemID {
		t.Errorf("Expected the override to still name item %s, got %v", itemID, overrides[0])
	}
}

// A training turned into a log only one saves an empty tree, which has to clear
// the items rather than leave the athlete on the old prescription.
func TestTrainingUpdate_ClearsEveryItemOnAnEmptyPayload(t *testing.T) {
	app, token := openItemsApp(t, "itemid9@test.com")
	training, _ := createNestedTraining(t, app, token)

	status, updated := putJSON(t, app, "/api/trainings/"+training["id"].(string), token, map[string]interface{}{
		"title": "Board session",
		"items": []map[string]interface{}{},
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 clearing the items, got %d: %v", status, updated)
	}

	stored := treeItemIDs(t, getTraining(t, app, token, training["id"].(string)))
	if len(stored) != 0 {
		t.Fatalf("Expected the stored tree to be empty, got %d items", len(stored))
	}
}
