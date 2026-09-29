package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"

	"github.com/gofiber/fiber/v3"
)

func listSessions(t *testing.T, app *fiber.App, token, query string) (int, []map[string]interface{}) {
	t.Helper()
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/sessions"+query, nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var list []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&list)
	return resp.StatusCode, list
}

func sessionListApp(t *testing.T) (*fiber.App, func() (string, string)) {
	t.Helper()
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
	})
	return app, func() (string, string) {
		return testutil.CreateTestUser(t, queries, "listreps@test.com")
	}
}

func rep(index int, averageWeight float64, isRest bool) map[string]interface{} {
	return map[string]interface{}{
		"average_weight": averageWeight,
		"is_rest":        isRest,
		"hand":           "right",
		"duration":       7,
		"target_weight":  0.0,
		"index":          index,
		"grip_position":  0,
	}
}

func createSessionWithReps(t *testing.T, app *fiber.App, token, name string, reps []map[string]interface{}) string {
	t.Helper()
	status, body := postJSON(t, app, "/api/sessions", token, map[string]interface{}{
		"name":      name,
		"activity":  0,
		"duration":  60,
		"rep_datas": reps,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating %s, got %d: %v", name, status, body)
	}
	return body["id"].(string)
}

func sessionWithID(t *testing.T, list []map[string]interface{}, id string) map[string]interface{} {
	t.Helper()
	for _, row := range list {
		if row["id"] == id {
			return row
		}
	}
	t.Fatalf("Expected session %s in %v", id, list)
	return nil
}

// The cheap listing is what the history screen reads on every open, so it has
// to keep leaving the reps off.
func TestSessions_ListWithoutIncludeCarriesNoReps(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, newUser := sessionListApp(t)
	_, token := newUser()

	createSessionWithReps(t, app, token, "Repeaters", []map[string]interface{}{rep(0, 20, false)})

	status, list := listSessions(t, app, token, "")
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", status)
	}
	if len(list) != 1 {
		t.Fatalf("Expected one session, got %v", list)
	}
	if _, ok := list[0]["rep_datas"]; ok {
		t.Errorf("Expected no rep_datas on the cheap listing, got %v", list[0]["rep_datas"])
	}
	if list[0]["rep_count"] != float64(1) {
		t.Errorf("Expected the rep count to stay on the row, got %v", list[0]["rep_count"])
	}
}

func TestSessions_ListWithIncludeRepsCarriesEachSessionsReps(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, newUser := sessionListApp(t)
	_, token := newUser()

	// Sent out of order, to show the listing hands them back in the order the
	// run recorded them.
	withReps := createSessionWithReps(t, app, token, "Repeaters", []map[string]interface{}{
		rep(2, 22, false),
		rep(0, 20, false),
		rep(1, 0, true),
	})
	other := createSessionWithReps(t, app, token, "Max hangs", []map[string]interface{}{rep(0, 40, false)})
	empty := createSessionWithReps(t, app, token, "Climbing", nil)

	status, list := listSessions(t, app, token, "?include=reps")
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", status)
	}
	if len(list) != 3 {
		t.Fatalf("Expected three sessions, got %v", list)
	}

	reps, ok := sessionWithID(t, list, withReps)["rep_datas"].([]interface{})
	if !ok || len(reps) != 3 {
		t.Fatalf("Expected three reps on %s, got %v", withReps, sessionWithID(t, list, withReps)["rep_datas"])
	}
	for i, raw := range reps {
		row := raw.(map[string]interface{})
		if row["index"] != float64(i) {
			t.Errorf("Expected rep %d at index %d, got %v", i, i, row["index"])
		}
		if row["session_id"] != withReps {
			t.Errorf("Expected rep %d to belong to %s, got %v", i, withReps, row["session_id"])
		}
	}
	if reps[2].(map[string]interface{})["average_weight"] != float64(22) {
		t.Errorf("Expected the last rep at 22 kg, got %v", reps[2])
	}

	otherReps, ok := sessionWithID(t, list, other)["rep_datas"].([]interface{})
	if !ok || len(otherReps) != 1 {
		t.Errorf("Expected one rep on %s, got %v", other, sessionWithID(t, list, other)["rep_datas"])
	}

	// Present and empty, not absent: absent is what the cheap listing says.
	emptyReps, ok := sessionWithID(t, list, empty)["rep_datas"].([]interface{})
	if !ok || len(emptyReps) != 0 {
		t.Errorf("Expected an empty rep list on %s, got %v", empty, sessionWithID(t, list, empty)["rep_datas"])
	}
}

func TestSessions_ListRefusesAnUnknownInclude(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, newUser := sessionListApp(t)
	_, token := newUser()

	for _, query := range []string{"?include=rep", "?include=reps,samples"} {
		status, _ := listSessions(t, app, token, query)
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for %s, got %d", query, status)
		}
	}
}

func TestSessions_ListWithIncludeRepsCarriesNoOtherUsersReps(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
	})
	_, ownerToken := testutil.CreateTestUser(t, queries, "listrepsowner@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "listrepsother@test.com")

	createSessionWithReps(t, app, ownerToken, "Owner", []map[string]interface{}{rep(0, 30, false), rep(1, 31, false)})
	mine := createSessionWithReps(t, app, otherToken, "Other", []map[string]interface{}{rep(0, 10, false)})

	status, list := listSessions(t, app, otherToken, "?include=reps")
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", status)
	}
	if len(list) != 1 || list[0]["id"] != mine {
		t.Fatalf("Expected only the caller's own session, got %v", list)
	}
	reps := list[0]["rep_datas"].([]interface{})
	if len(reps) != 1 || reps[0].(map[string]interface{})["average_weight"] != float64(10) {
		t.Errorf("Expected only the caller's own rep, got %v", reps)
	}
}
