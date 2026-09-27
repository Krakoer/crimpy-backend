package handler_test

import (
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// The session detail modal draws the assessments of one session. A weighted hang
// a coach opens from the session list has to read as the same ratio it reads as
// on the assessments tab two clicks away, which means the session read has to
// divide by the weigh-in the listing and the two date snapshot divide by. One
// rule, one function, three reads.

// sessionAssessments reads a session detail and answers with its assessments.
func sessionAssessments(t *testing.T, f snapshotFixture, url, token string) []map[string]interface{} {
	t.Helper()
	resp, err := f.app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, url, nil, token))
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 reading %s, got %d", url, resp.StatusCode)
	}
	var detail map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("Failed to decode the session detail: %v", err)
	}
	raw, ok := detail["assessments"].([]interface{})
	if !ok {
		t.Fatalf("Expected an assessments array on the session detail, got %v", detail)
	}
	results := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		result, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected an assessment object, got %v", item)
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		t.Fatalf("Expected the session to carry its assessments, got %v", detail)
	}
	return results
}

// The denominator is the last weigh-in at or before the session, which is not
// the first one the athlete ever recorded and not the latest one on file: a
// result measured in March divided by a January weight is a ratio the athlete
// never achieved, and so is one divided by a June weight.
func TestSessionDetail_CarriesTheWeighInAtOrBeforeTheSession(t *testing.T) {
	f := setupSnapshotFixture(t, "sessdenom1")

	f.recordBodyweight(t, "2026-01-05T08:00:00Z", 68)
	f.recordBodyweight(t, "2026-03-01T08:00:00Z", 71)
	f.recordBodyweight(t, "2026-06-01T08:00:00Z", 75)
	sessionID := f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	results := sessionAssessments(t, f, "/api/sessions/"+sessionID, f.userToken)
	if results[0]["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the March weigh-in on the March session, got %v", results[0]["bodyweight_kg"])
	}
	if results[0]["bodyweight_measured_at"] != "2026-03-01T08:00:00Z" {
		t.Errorf("Expected the March weigh-in date, got %v", results[0]["bodyweight_measured_at"])
	}
}

// The coach opens the same session from the client's session list, and reads it
// through an endpoint of its own. The two have to name one weigh-in, or the
// modal says one thing to the coach and another to the athlete.
func TestCoachSessionDetail_CarriesTheSameWeighIn(t *testing.T) {
	f := setupSnapshotFixture(t, "sessdenom2")

	f.recordBodyweight(t, "2026-01-05T08:00:00Z", 68)
	f.recordBodyweight(t, "2026-03-01T08:00:00Z", 71)
	sessionID := f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	// The coach weighs in too, nearer the athlete's session than the athlete's own
	// weigh-in is. The denominator belongs to whoever did the assessment, and a
	// lookup correlated on the caller rather than the session owner would put the
	// coach's weight on the client's session.
	status, body := postJSON(t, f.app, "/api/user/bodyweights", f.coachToken, map[string]interface{}{
		"weight_kg": 88, "measured_at": "2026-03-02T06:00:00Z",
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording the coach's own bodyweight, got %d: %v", status, body)
	}

	own := sessionAssessments(t, f, "/api/sessions/"+sessionID, f.userToken)
	coached := sessionAssessments(t, f,
		fmt.Sprintf("/api/coach/clients/%s/sessions/%s", f.userID, sessionID), f.coachToken)

	if coached[0]["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the athlete's March weigh-in for the coach too, got %v", coached[0]["bodyweight_kg"])
	}
	if coached[0]["bodyweight_kg"] != own[0]["bodyweight_kg"] {
		t.Errorf("Expected one weight for one session, got %v for the coach and %v for the athlete",
			coached[0]["bodyweight_kg"], own[0]["bodyweight_kg"])
	}
	if coached[0]["bodyweight_measured_at"] != own[0]["bodyweight_measured_at"] {
		t.Errorf("Expected one weigh-in date for one session, got %v for the coach and %v for the athlete",
			coached[0]["bodyweight_measured_at"], own[0]["bodyweight_measured_at"])
	}
}

// A weight that did not exist yet when the session was run is not a denominator,
// and the session read says so by leaving the fields out rather than reaching
// forward for the nearest one. The modal has wording of its own for that state.
func TestSessionDetail_OmitsTheWeightWhenNonePrecedesTheSession(t *testing.T) {
	f := setupSnapshotFixture(t, "sessdenom3")

	sessionID := f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)
	f.recordBodyweight(t, "2026-06-01T08:00:00Z", 75)

	results := sessionAssessments(t, f, "/api/sessions/"+sessionID, f.userToken)
	if _, present := results[0]["bodyweight_kg"]; present {
		t.Errorf("Expected no weight on a session run before the first weigh-in, got %v", results[0]["bodyweight_kg"])
	}
	if _, present := results[0]["bodyweight_measured_at"]; present {
		t.Errorf("Expected no weigh-in date without a weight, got %v", results[0]["bodyweight_measured_at"])
	}
}

// The three reads of one rule, on one result. The session detail, the listing the
// cards are drawn from and the snapshot the comparison panel is drawn from all
// divide by bodyweight_for_result, and this is what says so: one disagreement
// here is a coach reading two different ratios for one measurement.
func TestSessionDetail_AgreesWithTheListingAndTheSnapshot(t *testing.T) {
	f := setupSnapshotFixture(t, "sessdenom4")

	// A weigh-in months before, one the morning of the session and one the evening
	// of it: every branch of the rule has something to choose between.
	f.recordBodyweight(t, "2026-01-05T08:00:00Z", 68)
	f.recordBodyweight(t, "2026-03-02T07:00:00Z", 71)
	f.recordBodyweight(t, "2026-03-02T21:00:00Z", 72.5)
	sessionID := f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	onSession := sessionAssessments(t, f, "/api/sessions/"+sessionID, f.userToken)[0]
	onList := findListedResult(t, listAssessments(t, f, "/api/assessments", f.userToken), "2026-03-02T10:00:00Z")

	_, body := f.snapshot(t, "/api/assessments/at?date=2026-03-02", f.userToken)
	onSnapshot := findResult(snapshotResults(t, body), testutil.BuiltinMaxForceID, 0)
	if onSnapshot == nil {
		t.Fatalf("Expected the max force result on the snapshot, got %v", body)
	}

	// The morning weigh-in: the last one at or before the session, which is what
	// the result was actually pulled at.
	if onSession["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the morning weigh-in on the session read, got %v", onSession["bodyweight_kg"])
	}
	if onSession["bodyweight_measured_at"] != "2026-03-02T07:00:00Z" {
		t.Errorf("Expected the morning weigh-in date on the session read, got %v", onSession["bodyweight_measured_at"])
	}
	if onSession["bodyweight_kg"] != onList["bodyweight_kg"] {
		t.Errorf("Expected one weight for one result, got %v on the session read and %v on the listing",
			onSession["bodyweight_kg"], onList["bodyweight_kg"])
	}
	if onSession["bodyweight_measured_at"] != onList["bodyweight_measured_at"] {
		t.Errorf("Expected one weigh-in date for one result, got %v on the session read and %v on the listing",
			onSession["bodyweight_measured_at"], onList["bodyweight_measured_at"])
	}
	if onSession["bodyweight_kg"] != onSnapshot["right_bodyweight_kg"] {
		t.Errorf("Expected one weight for one result, got %v on the session read and %v on the snapshot",
			onSession["bodyweight_kg"], onSnapshot["right_bodyweight_kg"])
	}
	if onSession["bodyweight_measured_at"] != onSnapshot["right_bodyweight_measured_at"] {
		t.Errorf("Expected one weigh-in date for one result, got %v on the session read and %v on the snapshot",
			onSession["bodyweight_measured_at"], onSnapshot["right_bodyweight_measured_at"])
	}
}

// An athlete who weighs themselves after training rather than before still
// weighed that on the day, and the session read widens to the rest of the day the
// same way the other two do.
func TestSessionDetail_TakesAWeighInLaterTheSameDayWhenNothingPrecedes(t *testing.T) {
	f := setupSnapshotFixture(t, "sessdenom5")

	sessionID := f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)
	f.recordBodyweight(t, "2026-03-02T19:30:00Z", 71)

	results := sessionAssessments(t, f, "/api/sessions/"+sessionID, f.userToken)
	if results[0]["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the weigh-in taken after the session to count, got %v", results[0]["bodyweight_kg"])
	}
	if results[0]["bodyweight_measured_at"] != "2026-03-02T19:30:00Z" {
		t.Errorf("Expected the evening weigh-in date, got %v", results[0]["bodyweight_measured_at"])
	}
}

// A coach who is not the athlete's reads nothing, denominator included.
func TestCoachSessionDetail_RefusesAnotherCoachsAthlete(t *testing.T) {
	f := setupSnapshotFixture(t, "sessdenom6")

	f.recordBodyweight(t, "2026-03-01T08:00:00Z", 71)
	sessionID := f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	_, otherToken := testutil.CreateTestValidatedCoachUser(t, f.pool, f.queries, "sessdenom6other@test.com")
	resp, err := f.app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet,
		fmt.Sprintf("/api/coach/clients/%s/sessions/%s", f.userID, sessionID), nil, otherToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403 reading another coach's athlete's session, got %d", resp.StatusCode)
	}
}

// Recording a result is the one path that answers with an assessment before
// anything has read it back, and it has to answer with the same shape. An athlete
// who has never weighed in gets a created result with no denominator on it, not
// an error and not a weight the response invented.
func TestCreateAssessment_AnswersWithNoWeightWhenNoneIsOnFile(t *testing.T) {
	f := setupSnapshotFixture(t, "sessdenom7")

	status, session := postJSON(t, f.app, "/api/sessions", f.userToken, map[string]interface{}{
		"name": "Test day", "notes": "", "date": "2026-03-02T10:00:00Z", "is_assessment": true,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the session, got %d: %v", status, session)
	}

	status, created := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
		"session_id":    session["id"],
		"assessment_id": testutil.BuiltinMaxForceID,
		"grip_position": 0,
		"right_value":   25,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording a result with no weigh-in on file, got %d: %v", status, created)
	}
	if _, present := created["bodyweight_kg"]; present {
		t.Errorf("Expected no weight on a result recorded before any weigh-in, got %v", created["bodyweight_kg"])
	}
	if _, present := created["bodyweight_measured_at"]; present {
		t.Errorf("Expected no weigh-in date without a weight, got %v", created["bodyweight_measured_at"])
	}
	if created["right_value"] != float64(25) {
		t.Errorf("Expected the recorded value back on a result with no denominator, got %v", created["right_value"])
	}
}

// And when the athlete has weighed in, the recording path answers with the same
// weigh-in every later read of that result answers with, rather than leaving the
// fields out and making a fresh recording look like one with no weight on file.
func TestCreateAssessment_AnswersWithTheWeighInTheReadsWillName(t *testing.T) {
	f := setupSnapshotFixture(t, "sessdenom8")

	f.recordBodyweight(t, "2026-01-05T08:00:00Z", 68)
	f.recordBodyweight(t, "2026-03-01T08:00:00Z", 71)

	status, session := postJSON(t, f.app, "/api/sessions", f.userToken, map[string]interface{}{
		"name": "Test day", "notes": "", "date": "2026-03-02T10:00:00Z", "is_assessment": true,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the session, got %d: %v", status, session)
	}

	status, created := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
		"session_id":    session["id"],
		"assessment_id": testutil.BuiltinMaxForceID,
		"grip_position": 0,
		"right_value":   25,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording a result, got %d: %v", status, created)
	}
	if created["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the March weigh-in on the recorded result, got %v", created["bodyweight_kg"])
	}
	if created["bodyweight_measured_at"] != "2026-03-01T08:00:00Z" {
		t.Errorf("Expected the March weigh-in date on the recorded result, got %v", created["bodyweight_measured_at"])
	}

	read := sessionAssessments(t, f, fmt.Sprintf("/api/sessions/%v", session["id"]), f.userToken)
	if read[0]["bodyweight_kg"] != created["bodyweight_kg"] {
		t.Errorf("Expected the recorded result and the read of it to name one weight, got %v and %v",
			created["bodyweight_kg"], read[0]["bodyweight_kg"])
	}
	if read[0]["bodyweight_measured_at"] != created["bodyweight_measured_at"] {
		t.Errorf("Expected the recorded result and the read of it to name one weigh-in date, got %v and %v",
			created["bodyweight_measured_at"], read[0]["bodyweight_measured_at"])
	}
}
