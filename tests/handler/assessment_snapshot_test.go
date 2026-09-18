package handler_test

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// snapshotFixture wires the whole read path a two date comparison walks: the
// sessions that record results, the definitions that name them, the bodyweight
// series that is the denominator of a ratio, and the coach who reads all of it.
type snapshotFixture struct {
	app        *fiber.App
	pool       *pgxpool.Pool
	queries    *db.Queries
	coachID    string
	coachToken string
	userID     string
	userToken  string
}

func setupSnapshotFixture(t *testing.T, prefix string) snapshotFixture {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, prefix+"coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, prefix+"user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler:             handler.NewTrainingHandler(queries, pool),
		AssessmentHandler:           handler.NewAssessmentHandler(queries),
		AssessmentDefinitionHandler: handler.NewAssessmentDefinitionHandler(queries, pool),
		SessionHandler:              handler.NewSessionHandler(queries, pool),
		BodyweightHandler:           handler.NewBodyweightHandler(queries, pool),
		CoachHandler:                handler.NewCoachHandler(queries, pool),
	})

	return snapshotFixture{
		app:        app,
		pool:       pool,
		queries:    queries,
		coachID:    coachID,
		coachToken: coachToken,
		userID:     userID,
		userToken:  userToken,
	}
}

// recordAssessment logs an assessment session on a given day carrying one result.
func (f snapshotFixture) recordAssessment(t *testing.T, date, assessmentID string, grip int32, right, left *float32) {
	t.Helper()
	result := map[string]interface{}{"assessment_id": assessmentID, "grip_position": grip}
	if right != nil {
		result["right_value"] = *right
	}
	if left != nil {
		result["left_value"] = *left
	}
	status, body := postJSON(t, f.app, "/api/sessions", f.userToken, map[string]interface{}{
		"name":          "Test day",
		"notes":         "",
		"date":          date,
		"is_assessment": true,
		"assessments":   []map[string]interface{}{result},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording an assessment, got %d: %v", status, body)
	}
}

func (f snapshotFixture) recordBodyweight(t *testing.T, measuredAt string, weightKg float32) {
	t.Helper()
	status, body := postJSON(t, f.app, "/api/user/bodyweights", f.userToken, map[string]interface{}{
		"weight_kg": weightKg, "measured_at": measuredAt,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording a bodyweight, got %d: %v", status, body)
	}
}

func (f snapshotFixture) snapshot(t *testing.T, url, token string) (int, map[string]interface{}) {
	t.Helper()
	resp, err := f.app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, url, nil, token))
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func snapshotResults(t *testing.T, body map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := body["results"].([]interface{})
	if !ok {
		t.Fatalf("Expected a results array, got %v", body)
	}
	results := make([]map[string]interface{}, 0, len(raw))
	for _, entry := range raw {
		results = append(results, entry.(map[string]interface{}))
	}
	return results
}

func findResult(results []map[string]interface{}, assessmentID string, grip float64) map[string]interface{} {
	for _, result := range results {
		if result["assessment_id"] == assessmentID && result["grip_position"] == grip {
			return result
		}
	}
	return nil
}

func floatPtr(v float32) *float32 { return &v }

func TestAssessmentSnapshot_ReadsTheValueInEffectOnTheDate(t *testing.T) {
	f := setupSnapshotFixture(t, "snap1")

	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), floatPtr(24))
	f.recordAssessment(t, "2026-06-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(28), floatPtr(27))

	status, body := f.snapshot(t, "/api/assessments/at?date=2026-03-02", f.userToken)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d: %v", status, body)
	}
	results := snapshotResults(t, body)
	early := findResult(results, testutil.BuiltinMaxForceID, 0)
	if early == nil {
		t.Fatalf("Expected the March result, got %v", results)
	}
	if early["right_value"] != float64(25) || early["left_value"] != float64(24) {
		t.Errorf("Expected the March values, got %v", early)
	}

	// A day with no session of its own still answers, with the last values
	// measured before it, which is what makes an arbitrary date comparable.
	status, body = f.snapshot(t, "/api/assessments/at?date=2026-04-15", f.userToken)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d: %v", status, body)
	}
	carried := findResult(snapshotResults(t, body), testutil.BuiltinMaxForceID, 0)
	if carried == nil || carried["right_value"] != float64(25) {
		t.Fatalf("Expected the March value carried into April, got %v", carried)
	}
	// And it says when it was measured, so a reader can tell a value carried
	// forward from one taken on the day.
	if carried["right_measured_at"] != "2026-03-02T10:00:00Z" {
		t.Errorf("Expected the date the carried value was measured, got %v", carried["right_measured_at"])
	}

	status, body = f.snapshot(t, "/api/assessments/at?date=2026-06-02", f.userToken)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d: %v", status, body)
	}
	late := findResult(snapshotResults(t, body), testutil.BuiltinMaxForceID, 0)
	if late == nil || late["right_value"] != float64(28) || late["left_value"] != float64(27) {
		t.Fatalf("Expected the June values, got %v", late)
	}
}

// A session recorded during the day still counts for that day: a coach picking a
// date means the state of things that evening, not at midnight.
func TestAssessmentSnapshot_ADayIncludesWhatWasMeasuredDuringIt(t *testing.T) {
	f := setupSnapshotFixture(t, "snap2")

	f.recordAssessment(t, "2026-03-02T18:45:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	_, body := f.snapshot(t, "/api/assessments/at?date=2026-03-02", f.userToken)
	if findResult(snapshotResults(t, body), testutil.BuiltinMaxForceID, 0) == nil {
		t.Errorf("Expected an evening session to count for its own day, got %v", body)
	}

	// The day before it holds nothing, rather than reaching forward.
	_, body = f.snapshot(t, "/api/assessments/at?date=2026-03-01", f.userToken)
	if len(snapshotResults(t, body)) != 0 {
		t.Errorf("Expected nothing measured before the session, got %v", body)
	}
}

// The hands are tracked apart, so a later session carrying only one of them does
// not discard the other hand's last measurement.
func TestAssessmentSnapshot_KeepsAHandALaterSessionDidNotMeasure(t *testing.T) {
	f := setupSnapshotFixture(t, "snap3")

	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinCriticalForceID, 0, floatPtr(20), floatPtr(19))
	f.recordAssessment(t, "2026-06-02T10:00:00Z", testutil.BuiltinCriticalForceID, 0, floatPtr(22), nil)

	_, body := f.snapshot(t, "/api/assessments/at?date=2026-06-02", f.userToken)
	result := findResult(snapshotResults(t, body), testutil.BuiltinCriticalForceID, 0)
	if result == nil {
		t.Fatalf("Expected a result, got %v", body)
	}
	if result["right_value"] != float64(22) {
		t.Errorf("Expected the June right hand, got %v", result["right_value"])
	}
	if result["left_value"] != float64(19) {
		t.Errorf("Expected the March left hand kept, got %v", result["left_value"])
	}
	if result["left_measured_at"] != "2026-03-02T10:00:00Z" {
		t.Errorf("Expected the left hand dated to March, got %v", result["left_measured_at"])
	}
}

// A hang on a 20mm edge and one on a 10mm edge are different tests, so they are
// different rows rather than one overwriting the other.
func TestAssessmentSnapshot_SeparatesGrips(t *testing.T) {
	f := setupSnapshotFixture(t, "snap4")

	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)
	f.recordAssessment(t, "2026-03-03T10:00:00Z", testutil.BuiltinMaxForceID, 1, floatPtr(18), nil)

	_, body := f.snapshot(t, "/api/assessments/at?date=2026-03-03", f.userToken)
	results := snapshotResults(t, body)
	if len(results) != 2 {
		t.Fatalf("Expected one row per grip, got %v", results)
	}
	if r := findResult(results, testutil.BuiltinMaxForceID, 0); r == nil || r["right_value"] != float64(25) {
		t.Errorf("Expected the open hand result untouched, got %v", r)
	}
	if r := findResult(results, testutil.BuiltinMaxForceID, 1); r == nil || r["right_value"] != float64(18) {
		t.Errorf("Expected the second grip on its own row, got %v", r)
	}
}

// The bodyweight travels with the snapshot, and it is the one in effect then
// rather than the one the athlete carries now: a ratio read against today's
// weight would restate a result measured six months ago.
func TestAssessmentSnapshot_CarriesTheBodyweightInEffectThen(t *testing.T) {
	f := setupSnapshotFixture(t, "snap5")

	f.recordBodyweight(t, "2026-03-01T08:00:00Z", 71)
	f.recordBodyweight(t, "2026-06-01T08:00:00Z", 73.5)
	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	_, body := f.snapshot(t, "/api/assessments/at?date=2026-03-02", f.userToken)
	if body["bodyweight_kg"] != float64(71) {
		t.Errorf("Expected the March weight, got %v", body["bodyweight_kg"])
	}

	_, body = f.snapshot(t, "/api/assessments/at?date=2026-06-02", f.userToken)
	if body["bodyweight_kg"] != float64(73.5) {
		t.Errorf("Expected the June weight, got %v", body["bodyweight_kg"])
	}

	// Before any weigh-in it is absent rather than zero, which a reader has to
	// say out loud rather than divide by.
	_, body = f.snapshot(t, "/api/assessments/at?date=2026-02-01", f.userToken)
	if _, present := body["bodyweight_kg"]; present {
		t.Errorf("Expected no weight before the first measurement, got %v", body["bodyweight_kg"])
	}
}

func TestAssessmentSnapshot_RefusesAMissingOrUnreadableDate(t *testing.T) {
	f := setupSnapshotFixture(t, "snap6")

	for _, url := range []string{
		"/api/assessments/at",
		"/api/assessments/at?date=",
		"/api/assessments/at?date=last%20tuesday",
		"/api/assessments/at?date=02-03-2026",
	} {
		status, body := f.snapshot(t, url, f.userToken)
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for %s, got %d: %v", url, status, body)
		}
	}
}

func TestAssessmentSnapshot_CoachReadsAnEnrolledClient(t *testing.T) {
	f := setupSnapshotFixture(t, "snap7")

	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	url := fmt.Sprintf("/api/coach/clients/%s/assessments/at?date=2026-03-02", f.userID)
	status, body := f.snapshot(t, url, f.coachToken)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 for the athlete's own coach, got %d: %v", status, body)
	}
	if findResult(snapshotResults(t, body), testutil.BuiltinMaxForceID, 0) == nil {
		t.Errorf("Expected the client's result, got %v", body)
	}
}

func TestAssessmentSnapshot_CoachCannotReadSomebodyElsesClient(t *testing.T) {
	f := setupSnapshotFixture(t, "snap8")
	_, strangerToken := testutil.CreateTestValidatedCoachUser(t, f.pool, f.queries, "snap8stranger@test.com")

	url := fmt.Sprintf("/api/coach/clients/%s/assessments/at?date=2026-03-02", f.userID)
	status, body := f.snapshot(t, url, strangerToken)
	if status != fiber.StatusForbidden {
		t.Errorf("Expected 403 for a coach the athlete is not enrolled with, got %d: %v", status, body)
	}

	// The athlete is not a coach, so the coach route refuses them too.
	status, body = f.snapshot(t, url, f.userToken)
	if status != fiber.StatusForbidden {
		t.Errorf("Expected 403 for a non coach, got %d: %v", status, body)
	}
}

func TestAssessmentSnapshot_AnAthleteOnlyEverSeesTheirOwn(t *testing.T) {
	f := setupSnapshotFixture(t, "snap9")
	_, strangerToken := testutil.CreateTestUser(t, f.queries, "snap9stranger@test.com")

	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(25), nil)

	status, body := f.snapshot(t, "/api/assessments/at?date=2026-03-02", strangerToken)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 for another athlete reading their own empty history, got %d: %v", status, body)
	}
	if len(snapshotResults(t, body)) != 0 {
		t.Errorf("Expected an athlete to see none of somebody else's results, got %v", body)
	}
}

func TestBodyweightRelative_OnlyOnAnAssessmentInKilograms(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "bwrel1@test.com")
	app := assessmentApp(t, pool, queries)

	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Weighted hang",
		"items": []map[string]interface{}{{"type": "free", "comment": "to failure"}},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}

	status, body := postJSON(t, app, "/api/assessment-definitions", token, map[string]interface{}{
		"training_id": training["id"], "label": "Max dips", "prompt": "How many?",
		"unit": "repetitions", "bodyweight_relative": true,
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a ratio of a rep count, got %d: %v", status, body)
	}

	status, definition := postJSON(t, app, "/api/assessment-definitions", token, map[string]interface{}{
		"training_id": training["id"], "label": "Weighted hang", "prompt": "How much did you add?",
		"unit": "kilograms", "bodyweight_relative": true,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 for a ratio of a weight, got %d: %v", status, definition)
	}
	if definition["bodyweight_relative"] != true {
		t.Errorf("Expected the flag echoed back, got %v", definition["bodyweight_relative"])
	}
}

// The flag stores nothing, so unlike the unit and the hands it stays free to
// toggle once results exist: turning it on or off only changes how the same raw
// kilograms are drawn, for the whole history at once and reversibly.
func TestBodyweightRelative_StaysFreeToToggleOnceResultsExist(t *testing.T) {
	f := setupSnapshotFixture(t, "bwrel2")

	_, assessmentID := createAssessmentTraining(t, f.app, f.userToken, "Weighted hang", "kilograms", false)
	f.recordAssessment(t, "2026-03-02T10:00:00Z", assessmentID, 0, floatPtr(25), nil)

	update := map[string]interface{}{
		"label": "Weighted hang", "prompt": "How much did you add?",
		"unit": "kilograms", "per_hand": false, "bodyweight_relative": true,
	}
	payload, _ := json.Marshal(update)
	resp, err := f.app.Test(testutil.NewJSONRequestWithAuth(
		http.MethodPut, "/api/assessment-definitions/"+assessmentID, payload, f.userToken))
	if err != nil {
		t.Fatalf("Update request failed: %v", err)
	}
	var updated map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updated)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 turning the flag on with results on file, got %d: %v", resp.StatusCode, updated)
	}
	if updated["bodyweight_relative"] != true {
		t.Fatalf("Expected the flag set, got %v", updated)
	}

	// The unit beside it is frozen by those same results, which is the rule the
	// flag deliberately does not share.
	frozen := map[string]interface{}{
		"label": "Weighted hang", "prompt": "How much did you add?",
		"unit": "seconds", "per_hand": false, "bodyweight_relative": false,
	}
	payload, _ = json.Marshal(frozen)
	resp, err = f.app.Test(testutil.NewJSONRequestWithAuth(
		http.MethodPut, "/api/assessment-definitions/"+assessmentID, payload, f.userToken))
	if err != nil {
		t.Fatalf("Update request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("Expected 409 moving the unit with results on file, got %d", resp.StatusCode)
	}

	// And it can be turned back off, since nothing derived from it was stored.
	update["bodyweight_relative"] = false
	payload, _ = json.Marshal(update)
	resp, err = f.app.Test(testutil.NewJSONRequestWithAuth(
		http.MethodPut, "/api/assessment-definitions/"+assessmentID, payload, f.userToken))
	if err != nil {
		t.Fatalf("Update request failed: %v", err)
	}
	json.NewDecoder(resp.Body).Decode(&updated)
	if resp.StatusCode != fiber.StatusOK || updated["bodyweight_relative"] != false {
		t.Errorf("Expected the flag to come back off, got %d: %v", resp.StatusCode, updated)
	}
}

// The flag rides on every result row, so a reader formats a number as a ratio
// without fetching the definitions separately.
func TestBodyweightRelative_TravelsWithTheResults(t *testing.T) {
	f := setupSnapshotFixture(t, "bwrel3")

	_, assessmentID := createAssessmentTraining(t, f.app, f.userToken, "Weighted hang", "kilograms", false)
	payload, _ := json.Marshal(map[string]interface{}{
		"label": "Weighted hang", "prompt": "How much did you add?",
		"unit": "kilograms", "per_hand": false, "bodyweight_relative": true,
	})
	if _, err := f.app.Test(testutil.NewJSONRequestWithAuth(
		http.MethodPut, "/api/assessment-definitions/"+assessmentID, payload, f.userToken)); err != nil {
		t.Fatalf("Update request failed: %v", err)
	}
	f.recordAssessment(t, "2026-03-02T10:00:00Z", assessmentID, 0, floatPtr(25), nil)

	resp, err := f.app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/assessments", nil, f.userToken))
	if err != nil {
		t.Fatalf("List request failed: %v", err)
	}
	var list []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list) == 0 {
		t.Fatalf("Expected the recorded result, got %v", list)
	}
	if list[0]["bodyweight_relative"] != true {
		t.Errorf("Expected the flag on the result row, got %v", list[0])
	}

	_, body := f.snapshot(t, "/api/assessments/at?date=2026-03-02", f.userToken)
	result := findResult(snapshotResults(t, body), assessmentID, 0)
	if result == nil || result["bodyweight_relative"] != true {
		t.Errorf("Expected the flag on the snapshot row, got %v", result)
	}
}
