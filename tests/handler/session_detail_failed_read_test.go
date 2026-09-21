package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Krakoer/crimpy#130. A session detail is drawn from three reads, and one of
// them failing used to send an empty array in place of the collection it could
// not read. An empty array is the one wrong answer a client cannot tell from a
// right one: an empty assessments array reads as a session that recorded none,
// and an empty rep_datas array makes the coach's modal say out loud that the
// sensor measured nothing.
//
// So each collection now has three states on the wire, and all three are pinned
// here: populated is the rows, read and empty is [], and failed is absent. The
// middle one is what keeps the change honest. omitempty would have dropped the
// empty one too and put the ambiguity straight back.

// detailCollections are the three keys a session detail is drawn from.
var detailCollections = []string{"rep_datas", "assessments", "item_results"}

// sessionDetailQuery names the sqlc query behind each of them, which is what a
// fault is aimed at.
var sessionDetailQuery = map[string]string{
	"rep_datas":    "GetSessionRepDatas",
	"assessments":  "GetSessionAssessments",
	"item_results": "GetSessionItemResults",
}

// readSessionDetail reads a detail and answers with the decoded body, asserting
// only the status: what each collection came back as is the test's own business.
func readSessionDetail(t *testing.T, app *fiber.App, url, token string) map[string]interface{} {
	t.Helper()
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, url, nil, token))
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 reading %s, got %d", url, resp.StatusCode)
	}
	var detail map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("Failed to decode the detail of %s: %v", url, err)
	}
	return detail
}

// assertPopulated fails unless the key is an array holding rows.
func assertPopulated(t *testing.T, detail map[string]interface{}, key string) {
	t.Helper()
	rows, present := detail[key]
	if !present {
		t.Fatalf("Expected %s on the detail, got the key absent: %v", key, detail)
	}
	list, ok := rows.([]interface{})
	if !ok {
		t.Fatalf("Expected %s to be an array, got %v", key, rows)
	}
	if len(list) == 0 {
		t.Fatalf("Expected %s to carry rows, got an empty array", key)
	}
}

// assertEmptyArray fails unless the key is present and holds an empty array,
// which is what a collection the session genuinely holds none of answers with.
// A missing key here would be the bug this ticket removed, read backwards: the
// client would say the collection could not be loaded for a session that simply
// has none of it.
func assertEmptyArray(t *testing.T, detail map[string]interface{}, key string) {
	t.Helper()
	rows, present := detail[key]
	if !present {
		t.Fatalf("Expected %s present and empty on a session that holds none, got the key absent: %v", key, detail)
	}
	list, ok := rows.([]interface{})
	if !ok {
		t.Fatalf("Expected %s to be an array, got %v", key, rows)
	}
	if len(list) != 0 {
		t.Fatalf("Expected %s to be empty, got %d rows", key, len(list))
	}
}

// assertAbsent fails unless the key is missing from the object altogether, which
// is the whole point: JSON null and [] both read as an answer, and only absence
// says the collection could not be read.
func assertAbsent(t *testing.T, detail map[string]interface{}, key string) {
	t.Helper()
	if value, present := detail[key]; present {
		t.Fatalf("Expected %s left out of the detail once its read failed, got %v", key, value)
	}
}

// playedSessionWithEverything logs one session carrying all three collections,
// so the same session can answer the populated case and, with a fault aimed at
// one read, the failed case, with the two collections beside it proving the rest
// of the detail still came through.
func playedSessionWithEverything(t *testing.T, f snapshotFixture, assessmentID string) string {
	t.Helper()
	trainingID, _, exerciseID := createOpenTraining(t, f.app, f.userToken)

	status, session := postJSON(t, f.app, "/api/sessions", f.userToken, map[string]interface{}{
		"name":          "Everything day",
		"notes":         "All three collections",
		"date":          "2026-03-02T09:00:00Z",
		"is_assessment": true,
		"activity":      3,
		"origin":        "played",
		"duration":      420,
		"training_id":   trainingID,
		"rep_datas": []map[string]interface{}{
			{
				"average_weight": 30.0,
				"is_rest":        false,
				"hand":           "right",
				"duration":       7,
				"target_weight":  35.0,
				"index":          0,
				"grip_position":  0,
			},
		},
		"assessments": []map[string]interface{}{
			{"assessment_id": assessmentID, "grip_position": 0, "right_value": 42.0},
		},
		"item_results": []map[string]interface{}{
			{"training_item_id": exerciseID, "occurrence": 0, "reps": 11},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 logging the session, got %d: %v", status, session)
	}
	return session["id"].(string)
}

// loggedSessionWithNothing logs a session carrying none of the three, which is
// an ordinary hand-written log and the state [] is the honest answer for.
func loggedSessionWithNothing(t *testing.T, f snapshotFixture) string {
	t.Helper()
	status, session := postJSON(t, f.app, "/api/sessions", f.userToken, map[string]interface{}{
		"name":     "Nothing day",
		"notes":    "Wrote it down afterwards",
		"date":     "2026-03-03T09:00:00Z",
		"activity": 1,
		"duration": 1800,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 logging the session, got %d: %v", status, session)
	}
	return session["id"].(string)
}

// detailAppWithFailingQuery serves both session detail endpoints through the
// fault seam, with one named query failing and every other read running for
// real. Only this app carries the fault, so the session it reads was written
// through the healthy one.
func detailAppWithFailingQuery(t *testing.T, pool *pgxpool.Pool, queryName string) (*fiber.App, *testutil.FaultyDBTX) {
	t.Helper()
	queries, faulty := testutil.FailingQueries(pool, testutil.QueryNamed(queryName))
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
		CoachHandler:   handler.NewCoachHandler(queries, pool),
	})
	return app, faulty
}

// State one of three: a session holding all three collections sends all three.
func TestSessionDetail_SendsTheCollectionsTheSessionHolds(t *testing.T) {
	f := setupSnapshotFixture(t, "detailfull")
	_, assessmentID := createAssessmentTraining(t, f.app, f.userToken, "Max hang", "kilograms", false)
	sessionID := playedSessionWithEverything(t, f, assessmentID)

	reads := []struct {
		name  string
		url   string
		token string
	}{
		{"athlete", "/api/sessions/" + sessionID, f.userToken},
		{"coach", fmt.Sprintf("/api/coach/clients/%s/sessions/%s", f.userID, sessionID), f.coachToken},
	}
	for _, read := range reads {
		t.Run(read.name, func(t *testing.T) {
			detail := readSessionDetail(t, f.app, read.url, read.token)
			for _, key := range detailCollections {
				assertPopulated(t, detail, key)
			}
		})
	}
}

// State two of three: a session holding none of them sends [] for each, not an
// absent key. This is the state omitempty would have broken, and it is what
// makes absence mean one thing only.
func TestSessionDetail_SendsAnEmptyArrayForACollectionTheSessionHoldsNoneOf(t *testing.T) {
	f := setupSnapshotFixture(t, "detailempty")
	sessionID := loggedSessionWithNothing(t, f)

	athlete := readSessionDetail(t, f.app, "/api/sessions/"+sessionID, f.userToken)
	for _, key := range detailCollections {
		assertEmptyArray(t, athlete, key)
	}

	coach := readSessionDetail(t, f.app,
		fmt.Sprintf("/api/coach/clients/%s/sessions/%s", f.userID, sessionID), f.coachToken)
	for _, key := range detailCollections {
		assertEmptyArray(t, coach, key)
	}
}

// State three of three: a read that fails leaves its collection out of the
// object, the other two still answer, and the session itself is still served
// with a 200. Run once per collection, since each is its own read and each has
// its own line in the handler to get wrong.
func TestSessionDetail_LeavesAFailedCollectionOutOfTheAnswer(t *testing.T) {
	f := setupSnapshotFixture(t, "detailfault")
	_, assessmentID := createAssessmentTraining(t, f.app, f.userToken, "Max hang", "kilograms", false)
	sessionID := playedSessionWithEverything(t, f, assessmentID)

	for _, failed := range detailCollections {
		t.Run(failed, func(t *testing.T) {
			app, faulty := detailAppWithFailingQuery(t, f.pool, sessionDetailQuery[failed])
			detail := readSessionDetail(t, app, "/api/sessions/"+sessionID, f.userToken)

			if faulty.Injected() != 1 {
				t.Fatalf("Expected the fault to land once on %s, got %d", sessionDetailQuery[failed], faulty.Injected())
			}
			assertAbsent(t, detail, failed)
			for _, key := range detailCollections {
				if key != failed {
					assertPopulated(t, detail, key)
				}
			}
			session, ok := detail["session"].(map[string]interface{})
			if !ok {
				t.Fatalf("Expected the session still served beside the failed read, got %v", detail["session"])
			}
			if session["notes"] != "All three collections" {
				t.Errorf("Expected the session's notes still served, got %v", session["notes"])
			}
		})
	}
}

// The coach's endpoint reads through the same function, so it answers the same
// way. Pinned rather than assumed: the two handlers each build their own
// response and could drift.
func TestSessionDetail_CoachReadLeavesAFailedCollectionOutToo(t *testing.T) {
	f := setupSnapshotFixture(t, "detailcoachfault")
	_, assessmentID := createAssessmentTraining(t, f.app, f.userToken, "Max hang", "kilograms", false)
	sessionID := playedSessionWithEverything(t, f, assessmentID)

	for _, failed := range detailCollections {
		t.Run(failed, func(t *testing.T) {
			app, faulty := detailAppWithFailingQuery(t, f.pool, sessionDetailQuery[failed])
			url := fmt.Sprintf("/api/coach/clients/%s/sessions/%s", f.userID, sessionID)
			detail := readSessionDetail(t, app, url, f.coachToken)

			if faulty.Injected() != 1 {
				t.Fatalf("Expected the fault to land once on %s, got %d", sessionDetailQuery[failed], faulty.Injected())
			}
			assertAbsent(t, detail, failed)
			for _, key := range detailCollections {
				if key != failed {
					assertPopulated(t, detail, key)
				}
			}
		})
	}
}
