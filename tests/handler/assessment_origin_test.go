package handler_test

import (
	"crimpy/backend/tests/testutil"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// A result says whether it came from a test of the assessment or from a pull
// measured during a training, so a coach can tell a Max Force the athlete
// tested from one they kept off a training. Every result recorded before the
// distinction, and every client that does not send it, reads as a test.

// recordTraining logs an ordinary training session, which is what a result
// kept from a training is attached to.
func (f snapshotFixture) recordTraining(t *testing.T, date, token string) string {
	t.Helper()
	status, body := postJSON(t, f.app, "/api/sessions", token, map[string]interface{}{
		"name":          "Test repeaters",
		"notes":         "",
		"date":          date,
		"is_assessment": false,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording a training, got %d: %v", status, body)
	}
	sessionID, ok := body["id"].(string)
	if !ok {
		t.Fatalf("Expected the created session to carry an id, got %v", body)
	}
	return sessionID
}

func TestCreateAssessment_OriginDefaultsToTest(t *testing.T) {
	f := setupSnapshotFixture(t, "origin1")
	sessionID := f.recordTraining(t, "2026-03-02T10:00:00Z", f.userToken)

	status, body := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
		"session_id":    sessionID,
		"assessment_id": testutil.BuiltinMaxForceID,
		"right_value":   30,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, body)
	}
	if body["origin"] != "test" {
		t.Errorf("Expected a result posted without an origin to read as a test, got %v", body["origin"])
	}
}

func TestCreateAssessment_TrainingOriginReadsBackOnEveryPath(t *testing.T) {
	f := setupSnapshotFixture(t, "origin2")
	sessionID := f.recordTraining(t, "2026-03-02T10:00:00Z", f.userToken)

	status, body := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
		"session_id":    sessionID,
		"assessment_id": testutil.BuiltinMaxForceID,
		"right_value":   31.5,
		"grip_position": 0,
		"origin":        "training",
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, body)
	}
	if body["origin"] != "training" {
		t.Errorf("Expected the created result to carry its training origin, got %v", body["origin"])
	}

	own := findListedResult(t, listAssessments(t, f, "/api/assessments", f.userToken), "2026-03-02T10:00:00Z")
	if own["origin"] != "training" {
		t.Errorf("Expected the athlete's listing to carry the training origin, got %v", own["origin"])
	}

	coach := findListedResult(t, listAssessments(t, f, "/api/coach/clients/"+f.userID+"/assessments", f.coachToken), "2026-03-02T10:00:00Z")
	if coach["origin"] != "training" {
		t.Errorf("Expected the coach's listing to carry the training origin, got %v", coach["origin"])
	}

	readStatus, session := getJSON(t, f.app, "/api/sessions/"+sessionID, f.userToken)
	if readStatus != fiber.StatusOK {
		t.Fatalf("Expected 200 reading the session, got %d", readStatus)
	}
	results, ok := session["assessments"].([]interface{})
	if !ok || len(results) != 1 {
		t.Fatalf("Expected the session to carry its one result, got %v", session["assessments"])
	}
	if results[0].(map[string]interface{})["origin"] != "training" {
		t.Errorf("Expected the session read to carry the training origin, got %v", results[0])
	}
	if session["is_assessment"] == true {
		t.Errorf("Expected the training to stay a training once a result is kept from it, got %v", session["is_assessment"])
	}
}

func TestCreateAssessment_RefusesAnUnknownOrigin(t *testing.T) {
	f := setupSnapshotFixture(t, "origin3")
	sessionID := f.recordTraining(t, "2026-03-02T10:00:00Z", f.userToken)

	status, body := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
		"session_id":    sessionID,
		"assessment_id": testutil.BuiltinMaxForceID,
		"right_value":   30,
		"origin":        "guess",
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 on an unknown origin, got %d: %v", status, body)
	}
}

// Keeping a training pull does not open a way into another athlete's history:
// the session it names has to be the caller's own, whatever the origin.
func TestCreateAssessment_TrainingOriginOnAnotherUsersSessionIsForbidden(t *testing.T) {
	f := setupSnapshotFixture(t, "origin4")
	_, otherToken := testutil.CreateTestUser(t, f.queries, "origin4other@test.com")
	otherSession := f.recordTraining(t, "2026-03-02T10:00:00Z", otherToken)

	status, body := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
		"session_id":    otherSession,
		"assessment_id": testutil.BuiltinMaxForceID,
		"right_value":   30,
		"origin":        "training",
	})
	if status != fiber.StatusForbidden {
		t.Fatalf("Expected 403 recording against another athlete's session, got %d: %v", status, body)
	}
}

// The session create path takes results inline, and reads the origin the same
// way the standalone endpoint does.
func TestCreateSession_InlineResultCarriesItsOrigin(t *testing.T) {
	f := setupSnapshotFixture(t, "origin5")

	status, body := postJSON(t, f.app, "/api/sessions", f.userToken, map[string]interface{}{
		"name":          "Test repeaters",
		"notes":         "",
		"date":          "2026-03-02T10:00:00Z",
		"is_assessment": false,
		"assessments": []map[string]interface{}{{
			"assessment_id": testutil.BuiltinMaxForceID,
			"left_value":    29,
			"origin":        "training",
		}},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, body)
	}
	listed := findListedResult(t, listAssessments(t, f, "/api/assessments", f.userToken), "2026-03-02T10:00:00Z")
	if listed["origin"] != "training" {
		t.Errorf("Expected the inline result to carry its training origin, got %v", listed["origin"])
	}

	status, body = postJSON(t, f.app, "/api/sessions", f.userToken, map[string]interface{}{
		"name":          "Test repeaters",
		"notes":         "",
		"date":          "2026-03-03T10:00:00Z",
		"is_assessment": false,
		"assessments": []map[string]interface{}{{
			"assessment_id": testutil.BuiltinMaxForceID,
			"left_value":    29,
			"origin":        "guess",
		}},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 on an unknown inline origin, got %d: %v", status, body)
	}
}

// A pull kept from a Critical Force test's hardest pull lives on an assessment
// session, so the backend records provenance without policing which kind of
// session a training origin may sit on.
func TestCreateAssessment_TrainingOriginOnAnAssessmentSessionIsAccepted(t *testing.T) {
	f := setupSnapshotFixture(t, "origin6")
	sessionID := f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinCriticalForceID, 0, floatPtr(18), nil)

	status, body := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
		"session_id":    sessionID,
		"assessment_id": testutil.BuiltinMaxForceID,
		"right_value":   30,
		"origin":        "training",
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 keeping a pull on an assessment session, got %d: %v", status, body)
	}
	for _, item := range listAssessments(t, f, "/api/assessments", f.userToken) {
		if item["assessment_id"] == testutil.BuiltinMaxForceID && item["origin"] != "training" {
			t.Errorf("Expected the kept pull to read back as from a training, got %v", item["origin"])
		}
	}
}

func TestCreateSession_InlineResultWithoutOriginIsATest(t *testing.T) {
	f := setupSnapshotFixture(t, "origin7")
	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(30), nil)

	listed := findListedResult(t, listAssessments(t, f, "/api/assessments", f.userToken), "2026-03-02T10:00:00Z")
	if listed["origin"] != "test" {
		t.Errorf("Expected an inline result sent without an origin to read as a test, got %v", listed["origin"])
	}
}

// The comparison reads results through the snapshot, which carries each hand
// forward on its own. Each hand says where its value came from, so a coach
// comparing two dates can tell a kept pull from a retest.
func TestAssessmentSnapshot_CarriesEachHandsOrigin(t *testing.T) {
	f := setupSnapshotFixture(t, "origin8")
	f.recordAssessment(t, "2026-03-02T10:00:00Z", testutil.BuiltinMaxForceID, 0, floatPtr(40), floatPtr(38))
	sessionID := f.recordTraining(t, "2026-03-09T10:00:00Z", f.userToken)
	status, body := postJSON(t, f.app, "/api/assessments", f.userToken, map[string]interface{}{
		"session_id":    sessionID,
		"assessment_id": testutil.BuiltinMaxForceID,
		"right_value":   42,
		"grip_position": 0,
		"origin":        "training",
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, body)
	}

	for url, token := range map[string]string{
		"/api/assessments/at?date=2026-03-10":                                f.userToken,
		"/api/coach/clients/" + f.userID + "/assessments/at?date=2026-03-10": f.coachToken,
	} {
		status, snap := f.snapshot(t, url, token)
		if status != fiber.StatusOK {
			t.Fatalf("Expected 200 from %s, got %d", url, status)
		}
		result := findResult(snapshotResults(t, snap), testutil.BuiltinMaxForceID, 0)
		if result == nil {
			t.Fatalf("Expected a Max Force in %s, got %v", url, snap)
		}
		if result["right_origin"] != "training" || result["right_value"] != float64(42) {
			t.Errorf("%s: expected the kept right hand pull, got %v", url, result)
		}
		if result["left_origin"] != "test" || result["left_value"] != float64(38) {
			t.Errorf("%s: expected the tested left hand carried forward, got %v", url, result)
		}
	}
}
