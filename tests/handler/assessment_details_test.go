package handler_test

import (
	"crimpy/backend/tests/testutil"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// A result can carry what its test measured beyond the one value, as a JSON
// object: a Critical Force keeps its W' and per pull numbers there while the
// value stays the Critical Force. Every result recorded without it reads as
// having none.

func criticalForceDetails() map[string]interface{} {
	return map[string]interface{}{
		"definition":   "last6",
		"w_prime_kg_s": 512.5,
		"end_force_kg": 16.8,
		"pulls": []map[string]interface{}{
			{"mean_kg": 30.1, "peak_kg": 34.2, "end_kg": 27.5, "impulse_kg_s": 210.7, "late_off": false},
			{"mean_kg": nil, "peak_kg": 12.0, "end_kg": nil, "impulse_kg_s": 40.0, "late_off": true},
		},
	}
}

func TestCreateAssessment_DetailsReadBackOnEveryPath(t *testing.T) {
	f := setupSnapshotFixture(t, "details1")
	sessionID := f.recordTraining(t, "2026-03-02T10:00:00Z", f.userToken)

	status, body := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
		"session_id":    sessionID,
		"assessment_id": testutil.BuiltinCriticalForceID,
		"right_value":   18.5,
		"details":       criticalForceDetails(),
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, body)
	}
	assertCriticalForceDetails(t, "the created result", body["details"])

	own := findListedResult(t, listAssessments(t, f, "/api/assessments", f.userToken), "2026-03-02T10:00:00Z")
	assertCriticalForceDetails(t, "the athlete's listing", own["details"])

	coach := findListedResult(t, listAssessments(t, f, "/api/coach/clients/"+f.userID+"/assessments", f.coachToken), "2026-03-02T10:00:00Z")
	assertCriticalForceDetails(t, "the coach's listing", coach["details"])

	readStatus, session := getJSON(t, f.app, "/api/sessions/"+sessionID, f.userToken)
	if readStatus != fiber.StatusOK {
		t.Fatalf("Expected 200 reading the session, got %d", readStatus)
	}
	results, ok := session["assessments"].([]interface{})
	if !ok || len(results) != 1 {
		t.Fatalf("Expected the session to carry its one result, got %v", session["assessments"])
	}
	assertCriticalForceDetails(t, "the session read", results[0].(map[string]interface{})["details"])
}

func assertCriticalForceDetails(t *testing.T, where string, raw interface{}) {
	t.Helper()
	details, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected %s to carry details, got %v", where, raw)
	}
	if details["w_prime_kg_s"] != 512.5 || details["end_force_kg"] != 16.8 || details["definition"] != "last6" {
		t.Errorf("Expected %s to carry the W' and end force posted, got %v", where, details)
	}
	pulls, ok := details["pulls"].([]interface{})
	if !ok || len(pulls) != 2 {
		t.Fatalf("Expected %s to carry both pulls, got %v", where, details["pulls"])
	}
	second := pulls[1].(map[string]interface{})
	if second["late_off"] != true || second["mean_kg"] != nil {
		t.Errorf("Expected %s to keep the second pull as posted, got %v", where, second)
	}
}

func TestCreateAssessment_WithoutDetailsHasNone(t *testing.T) {
	f := setupSnapshotFixture(t, "details2")
	sessionID := f.recordTraining(t, "2026-03-02T10:00:00Z", f.userToken)

	for _, details := range []interface{}{nil, "absent"} {
		request := map[string]interface{}{
			"session_id":    sessionID,
			"assessment_id": testutil.BuiltinMaxForceID,
			"right_value":   30,
		}
		if details != "absent" {
			request["details"] = details
		}
		status, body := postJSON(t, f.app, "/api/assessments", f.userToken, request)
		if status != fiber.StatusCreated {
			t.Fatalf("Expected 201, got %d: %v", status, body)
		}
		if _, present := body["details"]; present {
			t.Errorf("Expected a result posted with details %v to carry none, got %v", details, body["details"])
		}
	}
	for _, item := range listAssessments(t, f, "/api/assessments", f.userToken) {
		if _, present := item["details"]; present {
			t.Errorf("Expected a listed result without details to leave the field out, got %v", item["details"])
		}
	}
}

func TestCreateAssessment_RefusesDetailsThatAreNotAnObject(t *testing.T) {
	f := setupSnapshotFixture(t, "details3")
	sessionID := f.recordTraining(t, "2026-03-02T10:00:00Z", f.userToken)

	oversized := map[string]interface{}{"notes": strings.Repeat("x", 70<<10)}
	for _, details := range []interface{}{[]int{1, 2}, "text", 12, oversized} {
		status, body := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
			"session_id":    sessionID,
			"assessment_id": testutil.BuiltinCriticalForceID,
			"right_value":   18.5,
			"details":       details,
		})
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected 400 on details %.40v, got %d: %v", details, status, body)
		}
	}
}

// Details do not open a way into another athlete's history: the session named
// still has to be the caller's own.
func TestCreateAssessment_DetailsOnAnotherUsersSessionIsForbidden(t *testing.T) {
	f := setupSnapshotFixture(t, "details4")
	_, otherToken := testutil.CreateTestUser(t, f.queries, "details4other@test.com")
	otherSession := f.recordTraining(t, "2026-03-02T10:00:00Z", otherToken)

	status, body := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
		"session_id":    otherSession,
		"assessment_id": testutil.BuiltinCriticalForceID,
		"right_value":   18.5,
		"details":       criticalForceDetails(),
	})
	if status != fiber.StatusForbidden {
		t.Fatalf("Expected 403 recording against another athlete's session, got %d: %v", status, body)
	}
}

// The session create path takes results inline, and reads details the same
// way the standalone endpoint does.
func TestCreateSession_InlineResultCarriesItsDetails(t *testing.T) {
	f := setupSnapshotFixture(t, "details5")

	status, body := postJSON(t, f.app, "/api/sessions", f.userToken, map[string]interface{}{
		"name":          "Test critical force",
		"notes":         "",
		"date":          "2026-03-02T10:00:00Z",
		"is_assessment": true,
		"assessments": []map[string]interface{}{{
			"assessment_id": testutil.BuiltinCriticalForceID,
			"left_value":    17,
			"details":       criticalForceDetails(),
		}},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, body)
	}
	listed := findListedResult(t, listAssessments(t, f, "/api/assessments", f.userToken), "2026-03-02T10:00:00Z")
	assertCriticalForceDetails(t, "the inline result", listed["details"])

	status, body = postJSON(t, f.app, "/api/sessions", f.userToken, map[string]interface{}{
		"name":          "Test critical force",
		"notes":         "",
		"date":          "2026-03-03T10:00:00Z",
		"is_assessment": true,
		"assessments": []map[string]interface{}{{
			"assessment_id": testutil.BuiltinCriticalForceID,
			"left_value":    17,
			"details":       []int{1},
		}},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 on inline details that are not an object, got %d: %v", status, body)
	}
}

// A comparison of two dates shows what each hand's standing result measured
// beyond its value, from that result: the left hand's details do not answer
// for the right, and a result posted without details carries none.
func TestAssessmentSnapshot_CarriesEachHandsDetails(t *testing.T) {
	f := setupSnapshotFixture(t, "details6")
	status, body := postJSON(t, f.app, "/api/sessions", f.userToken, map[string]interface{}{
		"name":          "Test critical force",
		"notes":         "",
		"date":          "2026-03-02T10:00:00Z",
		"is_assessment": true,
		"assessments": []map[string]interface{}{{
			"assessment_id": testutil.BuiltinCriticalForceID,
			"grip_position": 0,
			"right_value":   18.5,
			"details":       criticalForceDetails(),
		}},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, body)
	}
	f.recordAssessment(t, "2026-03-03T10:00:00Z", testutil.BuiltinCriticalForceID, 0, nil, floatPtr(17))

	for _, url := range []string{
		"/api/assessments/at?date=2026-03-04",
		"/api/coach/clients/" + f.userID + "/assessments/at?date=2026-03-04",
	} {
		token := f.userToken
		if strings.Contains(url, "/coach/") {
			token = f.coachToken
		}
		status, body := f.snapshot(t, url, token)
		if status != fiber.StatusOK {
			t.Fatalf("Expected 200 on %s, got %d: %v", url, status, body)
		}
		item := findResult(snapshotResults(t, body), testutil.BuiltinCriticalForceID, 0)
		if item == nil {
			t.Fatalf("Expected a Critical Force in the snapshot at %s, got %v", url, body)
		}
		assertCriticalForceDetails(t, "the right hand of "+url, item["right_details"])
		if _, present := item["left_details"]; present {
			t.Errorf("Expected the left hand's result, posted without details, to carry none at %s, got %v", url, item["left_details"])
		}
	}
}
