package handler_test

import (
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// The assessment listing is what the coachee page draws its cards, its history
// table and its chart from. A bodyweight relative result read from it has to
// divide by the same weigh-in the two date comparison divides by, or one screen
// prints two different ratios for one measurement.

func listAssessments(t *testing.T, f snapshotFixture, url, token string) []map[string]interface{} {
	t.Helper()
	resp, err := f.app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, url, nil, token))
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 listing assessments, got %d", resp.StatusCode)
	}
	var list []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("Failed to decode the listing: %v", err)
	}
	return list
}

func findListedResult(t *testing.T, list []map[string]interface{}, sessionDate string) map[string]interface{} {
	t.Helper()
	for _, item := range list {
		if item["session_date"] == sessionDate {
			return item
		}
	}
	t.Fatalf("Expected a result measured on %s, got %v", sessionDate, list)
	return nil
}

// The denominator is the last weigh-in at or before the result, which is not the
// latest one on file: a result measured in March divided by a June weight is a
// ratio the athlete never achieved.
func TestAssessmentList_CarriesTheWeighInAtOrBeforeTheResult(t *testing.T) {
	f := setupSnapshotFixture(t, "denom1")

	f.recordBodyweight(t, "2026-03-01T08:00:00Z", 71)
	f.recordBodyweight(t, "2026-06-01T08:00:00Z", 75)
	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)
	f.recordAssessment(t, "2026-06-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(28), nil)

	list := listAssessments(t, f, "/api/assessments", f.userToken)

	march := findListedResult(t, list, "2026-03-02T10:00:00Z")
	if march["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the March weigh-in on the March result, got %v", march["bodyweight_kg"])
	}
	if march["bodyweight_measured_at"] != "2026-03-01T08:00:00Z" {
		t.Errorf("Expected the March weigh-in date, got %v", march["bodyweight_measured_at"])
	}

	june := findListedResult(t, list, "2026-06-02T10:00:00Z")
	if june["bodyweight_kg"] != float64(75) {
		t.Errorf("Expected the June weigh-in on the June result, got %v", june["bodyweight_kg"])
	}
	if june["bodyweight_measured_at"] != "2026-06-01T08:00:00Z" {
		t.Errorf("Expected the June weigh-in date, got %v", june["bodyweight_measured_at"])
	}
}

// A weight that did not exist yet when the result was measured is not a
// denominator, and the listing says so by leaving the field out rather than
// reaching forward for the nearest one.
func TestAssessmentList_OmitsTheWeightWhenNonePrecedesTheResult(t *testing.T) {
	f := setupSnapshotFixture(t, "denom2")

	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)
	f.recordBodyweight(t, "2026-06-01T08:00:00Z", 75)

	list := listAssessments(t, f, "/api/assessments", f.userToken)
	march := findListedResult(t, list, "2026-03-02T10:00:00Z")
	if _, present := march["bodyweight_kg"]; present {
		t.Errorf("Expected no weight on a result measured before the first weigh-in, got %v", march["bodyweight_kg"])
	}
	if _, present := march["bodyweight_measured_at"]; present {
		t.Errorf("Expected no weigh-in date without a weight, got %v", march["bodyweight_measured_at"])
	}
}

// An athlete who weighs themselves after training rather than before still
// weighed that on the day, so the search widens to the rest of the day when
// nothing precedes the result. The same widening the snapshot does.
func TestAssessmentList_TakesAWeighInLaterTheSameDayWhenNothingPrecedes(t *testing.T) {
	f := setupSnapshotFixture(t, "denom3")

	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)
	f.recordBodyweight(t, "2026-03-02T19:30:00Z", 71)

	list := listAssessments(t, f, "/api/assessments", f.userToken)
	march := findListedResult(t, list, "2026-03-02T10:00:00Z")
	if march["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the weigh-in taken after the session to count, got %v", march["bodyweight_kg"])
	}
	if march["bodyweight_measured_at"] != "2026-03-02T19:30:00Z" {
		t.Errorf("Expected the evening weigh-in date, got %v", march["bodyweight_measured_at"])
	}
}

// The morning weigh-in stays the denominator for an athlete who also weighs in
// at night: the widening only applies where nothing precedes the result.
func TestAssessmentList_PrefersTheMorningWeighInOverTheEveningOne(t *testing.T) {
	f := setupSnapshotFixture(t, "denom4")

	f.recordBodyweight(t, "2026-03-02T07:00:00Z", 71)
	f.recordBodyweight(t, "2026-03-02T21:00:00Z", 72.5)
	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	list := listAssessments(t, f, "/api/assessments", f.userToken)
	march := findListedResult(t, list, "2026-03-02T10:00:00Z")
	if march["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the morning weigh-in, got %v", march["bodyweight_kg"])
	}
	if march["bodyweight_measured_at"] != "2026-03-02T07:00:00Z" {
		t.Errorf("Expected the morning weigh-in date, got %v", march["bodyweight_measured_at"])
	}
}

// The coach reads the same listing the athlete does, and has to read the same
// denominator: the cards the coach sees are drawn from this endpoint.
func TestCoachAssessmentList_CarriesTheSameDenominator(t *testing.T) {
	f := setupSnapshotFixture(t, "denom5")

	f.recordBodyweight(t, "2026-03-01T08:00:00Z", 71)
	f.recordBodyweight(t, "2026-06-01T08:00:00Z", 75)
	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	// The coach weighs in too, nearer the athlete's result than the athlete's own
	// weigh-in is. The denominator belongs to whoever did the assessment, and a
	// lookup correlated on the caller rather than the session owner would put the
	// coach's weight on the client's card.
	status, body := postJSON(t, f.app, "/api/user/bodyweights", f.coachToken, map[string]interface{}{
		"weight_kg": 88, "measured_at": "2026-03-02T06:00:00Z",
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording the coach's own bodyweight, got %d: %v", status, body)
	}

	list := listAssessments(t, f, fmt.Sprintf("/api/coach/clients/%s/assessments", f.userID), f.coachToken)
	march := findListedResult(t, list, "2026-03-02T10:00:00Z")
	if march["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the athlete's March weigh-in for the coach too, got %v", march["bodyweight_kg"])
	}
	if march["bodyweight_measured_at"] != "2026-03-01T08:00:00Z" {
		t.Errorf("Expected the athlete's March weigh-in date for the coach too, got %v", march["bodyweight_measured_at"])
	}
}

// The listing and the snapshot are two reads of one rule, and a client asks the
// snapshot for a day. They have to name the same weigh-in for the same result,
// or the cards and the comparison panel below them print two different ratios.
func TestAssessmentDenominator_ListAndSnapshotAgreeForADay(t *testing.T) {
	f := setupSnapshotFixture(t, "denom9")

	// A weigh-in the morning of the result, one the evening of it, and one from
	// months before: every branch of the rule has something to choose between.
	f.recordBodyweight(t, "2026-01-05T08:00:00Z", 68)
	f.recordBodyweight(t, "2026-03-02T07:00:00Z", 71)
	f.recordBodyweight(t, "2026-03-02T21:00:00Z", 72.5)
	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	list := listAssessments(t, f, "/api/assessments", f.userToken)
	listed := findListedResult(t, list, "2026-03-02T10:00:00Z")

	_, body := f.snapshot(t, "/api/assessments/at?date=2026-03-02", f.userToken)
	result := findResult(snapshotResults(t, body), testutil.BuiltinMaxForceID, 0)
	if result == nil {
		t.Fatalf("Expected the max force result on the snapshot, got %v", body)
	}

	if listed["bodyweight_kg"] != result["right_bodyweight_kg"] {
		t.Errorf("Expected one weight for one result, got %v on the listing and %v on the snapshot",
			listed["bodyweight_kg"], result["right_bodyweight_kg"])
	}
	if listed["bodyweight_measured_at"] != result["right_bodyweight_measured_at"] {
		t.Errorf("Expected one weigh-in date for one result, got %v on the listing and %v on the snapshot",
			listed["bodyweight_measured_at"], result["right_bodyweight_measured_at"])
	}
	if listed["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the morning weigh-in on both, got %v", listed["bodyweight_kg"])
	}
}

// A result measured before the athlete ever weighed in has no denominator on
// either read, rather than one read inventing one.
func TestAssessmentDenominator_ListAndSnapshotAgreeOnHavingNoWeight(t *testing.T) {
	f := setupSnapshotFixture(t, "denom10")

	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)
	f.recordBodyweight(t, "2026-06-01T08:00:00Z", 75)

	list := listAssessments(t, f, "/api/assessments", f.userToken)
	listed := findListedResult(t, list, "2026-03-02T10:00:00Z")
	_, body := f.snapshot(t, "/api/assessments/at?date=2026-03-02", f.userToken)
	result := findResult(snapshotResults(t, body), testutil.BuiltinMaxForceID, 0)
	if result == nil {
		t.Fatalf("Expected the max force result on the snapshot, got %v", body)
	}

	_, onList := listed["bodyweight_kg"]
	_, onSnapshot := result["right_bodyweight_kg"]
	if onList || onSnapshot {
		t.Errorf("Expected no weight on either read, got list=%v snapshot=%v",
			listed["bodyweight_kg"], result["right_bodyweight_kg"])
	}
}

// A coach who is not the athlete's reads nothing, denominator included.
func TestCoachAssessmentList_RefusesAnotherCoachsAthlete(t *testing.T) {
	f := setupSnapshotFixture(t, "denom6")

	f.recordBodyweight(t, "2026-03-01T08:00:00Z", 71)
	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	_, otherToken := testutil.CreateTestValidatedCoachUser(t, f.pool, f.queries, "denom6other@test.com")
	resp, err := f.app.Test(testutil.NewJSONRequestWithAuth(
		http.MethodGet, fmt.Sprintf("/api/coach/clients/%s/assessments", f.userID), nil, otherToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403 reading another coach's athlete, got %d", resp.StatusCode)
	}
}

// The snapshot the comparison panel is drawn from says when its denominator was
// weighed, per hand, because the two hands can come from sessions months apart
// and each carries the weight of its own day.
func TestAssessmentSnapshot_SaysWhenEachDenominatorWasWeighed(t *testing.T) {
	f := setupSnapshotFixture(t, "denom7")

	f.recordBodyweight(t, "2026-03-01T08:00:00Z", 71)
	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), floatPtr(24))
	f.recordBodyweight(t, "2026-06-01T08:00:00Z", 75)
	f.recordAssessment(t, "2026-06-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(28), nil)

	_, body := f.snapshot(t, "/api/assessments/at?date=2026-06-10", f.userToken)
	result := findResult(snapshotResults(t, body), testutil.BuiltinMaxForceID, 0)
	if result == nil {
		t.Fatalf("Expected the max force result, got %v", body)
	}
	if result["right_bodyweight_measured_at"] != "2026-06-01T08:00:00Z" {
		t.Errorf("Expected the June weigh-in date on the retested hand, got %v", result["right_bodyweight_measured_at"])
	}
	if result["left_bodyweight_measured_at"] != "2026-03-01T08:00:00Z" {
		t.Errorf("Expected the March weigh-in date on the hand not retested, got %v", result["left_bodyweight_measured_at"])
	}
}

// No weight means no date: a date on its own would read as a weigh-in the
// response never named.
func TestAssessmentSnapshot_OmitsTheWeighInDateWithTheWeight(t *testing.T) {
	f := setupSnapshotFixture(t, "denom8")

	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)
	f.recordBodyweight(t, "2026-06-01T08:00:00Z", 75)

	_, body := f.snapshot(t, "/api/assessments/at?date=2026-03-10", f.userToken)
	result := findResult(snapshotResults(t, body), testutil.BuiltinMaxForceID, 0)
	if result == nil {
		t.Fatalf("Expected the max force result, got %v", body)
	}
	if _, present := result["right_bodyweight_measured_at"]; present {
		t.Errorf("Expected no weigh-in date without a weight, got %v", result["right_bodyweight_measured_at"])
	}
}
