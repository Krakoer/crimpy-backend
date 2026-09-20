package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// recordableSetup spans the whole recordable rule in one fixture: the
// assessments Crimpy ships, one the athlete wrote, one their coach wrote and
// prescribed to them, one their coach wrote and never prescribed, and one
// written by a coach they have nothing to do with.
type recordableSetup struct {
	App                  *fiber.App
	CoachToken           string
	UserToken            string
	UserID               string
	ProgramID            string
	OwnID                string
	OwnTrainingID        string
	PrescribedID         string
	PrescribedTrainingID string
	UnprescribedID       string
	StrangerID           string
}

func setupRecordable(t *testing.T, prefix string) recordableSetup {
	t.Helper()
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, prefix+"coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, prefix+"user@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	_, strangerToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, prefix+"stranger@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:              handler.NewSessionHandler(queries, pool),
		TrainingHandler:             handler.NewTrainingHandler(queries, pool),
		ProgramHandler:              handler.NewProgramHandler(queries, pool),
		AssessmentHandler:           handler.NewAssessmentHandler(queries),
		AssessmentDefinitionHandler: handler.NewAssessmentDefinitionHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	ownTrainingID, ownID := createAssessmentTraining(t, app, userToken, "Athlete pull up pyramid", "repetitions", false)
	prescribedTrainingID, prescribedID := createAssessmentTraining(t, app, coachToken, "Coach lock off", "seconds", true)
	_, unprescribedID := createAssessmentTraining(t, app, coachToken, "Coach dead hang", "kilograms", false)
	_, strangerID := createAssessmentTraining(t, app, strangerToken, "Stranger max hang", "kilograms", false)

	prescribeSession(t, app, coachToken, userID, programID, prescribedTrainingID, nil)

	return recordableSetup{
		App:                  app,
		CoachToken:           coachToken,
		UserToken:            userToken,
		UserID:               userID,
		ProgramID:            programID,
		OwnID:                ownID,
		OwnTrainingID:        ownTrainingID,
		PrescribedID:         prescribedID,
		PrescribedTrainingID: prescribedTrainingID,
		UnprescribedID:       unprescribedID,
		StrangerID:           strangerID,
	}
}

// listAssessmentDefinitions reads a listing keyed by assessment id, so a test
// can ask whether a row is there without depending on where it sorted.
func listAssessmentDefinitions(t *testing.T, app *fiber.App, token, query string) map[string]map[string]interface{} {
	t.Helper()
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/assessment-definitions"+query, nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 listing the assessments, got %d", resp.StatusCode)
	}
	var list []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("Could not decode the listing: %v", err)
	}
	byID := make(map[string]map[string]interface{}, len(list))
	for _, definition := range list {
		byID[definition["id"].(string)] = definition
	}
	return byID
}

func builtinAssessmentIDs() []string {
	return []string{
		testutil.BuiltinCriticalForceID,
		testutil.BuiltinMaxForceID,
		testutil.BuiltinEndurance60ID,
	}
}

// The flag widens the catalog rather than asking a different question: what the
// plain listing serves is still there, with the prescribed ones added.
func TestAssessmentDefinitions_RecordableAddsThePrescribedOnes(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	s := setupRecordable(t, "recwiden")

	catalog := listAssessmentDefinitions(t, s.App, s.UserToken, "")
	recordable := listAssessmentDefinitions(t, s.App, s.UserToken, "?recordable=true")

	for _, id := range append(builtinAssessmentIDs(), s.OwnID) {
		if _, listed := catalog[id]; !listed {
			t.Errorf("Expected %s in the catalog", id)
		}
		if _, listed := recordable[id]; !listed {
			t.Errorf("Expected %s to stay listed when the set widens", id)
		}
	}

	if _, listed := catalog[s.PrescribedID]; listed {
		t.Errorf("Expected the catalog to stop at the builtins and the caller's own")
	}
	prescribed, listed := recordable[s.PrescribedID]
	if !listed {
		t.Fatalf("Expected the prescribed coach assessment to be recordable, got %v", recordable)
	}
	if prescribed["training_id"] != s.PrescribedTrainingID {
		t.Errorf("Expected the training that measures it, got %v", prescribed["training_id"])
	}
	// Without this the row names a training the athlete cannot read: a coach's
	// training is only served under a program of theirs.
	if prescribed["program_id"] != s.ProgramID {
		t.Errorf("Expected the program that reads the training, got %v", prescribed["program_id"])
	}

	if _, named := recordable[s.OwnID]["program_id"]; named {
		t.Errorf("Expected no program on an assessment the athlete owns outright")
	}
	if _, named := recordable[testutil.BuiltinMaxForceID]["program_id"]; named {
		t.Errorf("Expected no program on an assessment Crimpy ships")
	}

	// The pair is served so the athlete can read the training under it, which
	// is the only reason the program id is on the row at all. The listing and
	// GetMyProgramTraining authorize through predicates written apart, so this
	// closes the loop rather than trusting they still match.
	resp, err := s.App.Test(testutil.NewJSONRequestWithAuth(http.MethodGet,
		"/api/user/programs/"+prescribed["program_id"].(string)+"/trainings/"+prescribed["training_id"].(string),
		nil, s.UserToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected the program the listing named to read the training, got %d", resp.StatusCode)
	}
}

// The listing is the recordable set and not a superset of it: an assessment a
// coach wrote reaches the athlete only through a program that prescribed its
// training, and one written by a stranger never does.
func TestAssessmentDefinitions_RecordableLeavesOutWhatWasNeverPrescribed(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	s := setupRecordable(t, "recnarrow")

	recordable := listAssessmentDefinitions(t, s.App, s.UserToken, "?recordable=true")

	if _, listed := recordable[s.UnprescribedID]; listed {
		t.Errorf("Expected an assessment the coach never prescribed to be absent")
	}
	if _, listed := recordable[s.StrangerID]; listed {
		t.Errorf("Expected an assessment of a coach the athlete has nothing to do with to be absent")
	}
}

// What the listing offers and what the record path accepts are the same rule,
// asked of the whole set and of one member. They are served by one query for
// that reason, and this is what says they still answer alike: an assessment
// offered and then refused is a dead end the athlete only meets once they have
// already pulled.
func TestAssessmentDefinitions_RecordableMatchesWhatMayBeRecorded(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	s := setupRecordable(t, "recagree")

	recordable := listAssessmentDefinitions(t, s.App, s.UserToken, "?recordable=true")
	sessionID := playSession(t, s.App, s.UserToken, nil)["id"].(string)

	spanning := append(builtinAssessmentIDs(), s.OwnID, s.PrescribedID, s.UnprescribedID, s.StrangerID)
	for _, id := range spanning {
		_, listed := recordable[id]
		status, body := postJSON(t, s.App, "/api/assessments", s.UserToken, map[string]interface{}{
			"session_id":    sessionID,
			"assessment_id": id,
			"right_value":   12.5,
		})
		accepted := status == fiber.StatusCreated
		if accepted != listed {
			t.Errorf("Assessment %s is listed as recordable=%v but recording it answered %d: %v", id, listed, status, body)
		}
	}
}

func TestAssessmentDefinitions_RecordableFalseServesTheCatalog(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	s := setupRecordable(t, "recfalse")

	off := listAssessmentDefinitions(t, s.App, s.UserToken, "?recordable=false")

	if _, listed := off[s.OwnID]; !listed {
		t.Errorf("Expected the caller's own assessment in the catalog")
	}
	if _, listed := off[s.PrescribedID]; listed {
		t.Errorf("Expected recordable=false to serve the catalog, not the recordable set")
	}
}

func TestAssessmentDefinitions_RecordableRefusesAValueThatIsNotABoolean(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	s := setupRecordable(t, "recbogus")

	resp, err := s.App.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/assessment-definitions?recordable=perhaps", nil, s.UserToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for a recordable that is not a boolean, got %d", resp.StatusCode)
	}
}

// unit_locked is a per definition question neither listing query answers, and
// both listings fill it in. One definition must read the same whichever of them
// served it, or a client is told the unit is free on one screen and frozen on
// the next.
func TestAssessmentDefinitions_RecordableReportsTheSameUnitLockAsTheCatalog(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	s := setupRecordable(t, "reclock")

	lockedIn := func(query string) bool {
		listing := listAssessmentDefinitions(t, s.App, s.UserToken, query)
		definition, listed := listing[s.OwnID]
		if !listed {
			t.Fatalf("Expected the caller's own assessment in the listing %q", query)
		}
		return definition["unit_locked"] == true
	}

	if lockedIn("") || lockedIn("?recordable=true") {
		t.Fatalf("Expected a fresh assessment to be free to re-unit in both listings")
	}

	// A measured result freezes it, since the number was read under that unit.
	sessionID := playSession(t, s.App, s.UserToken, nil)["id"].(string)
	status, body := postJSON(t, s.App, "/api/assessments", s.UserToken, map[string]interface{}{
		"session_id":    sessionID,
		"assessment_id": s.OwnID,
		"right_value":   9.0,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording a result, got %d: %v", status, body)
	}

	if !lockedIn("") {
		t.Errorf("Expected the catalog to report the unit locked once a result was measured")
	}
	if !lockedIn("?recordable=true") {
		t.Errorf("Expected the recordable listing to report the unit locked too")
	}
}

// The program named is the block the athlete is running now, which is the one
// whose week overrides name the assessments the training itself does not. Any
// program that prescribes the training authorizes reading it, so this is not an
// access question: reading it under a block they have moved on from, or have
// not started, leaves a prescribed percentage with nothing to label it.
//
// The dates are relative to today so the test does not rot, and the block that
// should win is created before the one that should not, so an order on the row
// creation time answers differently whichever way it is pointed.
func TestAssessmentDefinitions_RecordableNamesTheProgramInProgress(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	s := setupRecordable(t, "recinprog")

	today := time.Now().UTC()
	current := createProgramStarting(t, s, mondayOnOrBefore(today.AddDate(0, 0, -5)))
	planned := createProgramStarting(t, s, mondayOnOrBefore(today.AddDate(0, 0, 60)))
	for _, programID := range []string{current, planned} {
		prescribeSession(t, s.App, s.CoachToken, s.UserID, programID, s.PrescribedTrainingID, nil)
	}

	recordable := listAssessmentDefinitions(t, s.App, s.UserToken, "?recordable=true")
	named := recordable[s.PrescribedID]["program_id"]
	if named != current {
		switch named {
		case planned:
			t.Errorf("Expected the block in progress, got the one starting in two months")
		case s.ProgramID:
			t.Errorf("Expected the block in progress, got the one that started months ago")
		default:
			t.Errorf("Expected the block in progress %v, got %v", current, named)
		}
	}
}

// createProgramStarting is createTestProgram with a start date of its own, so a
// test can place a block before or after today.
func createProgramStarting(t *testing.T, s recordableSetup, start time.Time) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"name":       "Block from " + start.Format(time.DateOnly),
		"start_date": start.Format(time.DateOnly),
	})
	resp, err := s.App.Test(testutil.NewJSONRequestWithAuth(http.MethodPost,
		"/api/coach/clients/"+s.UserID+"/programs", body, s.CoachToken))
	if err != nil {
		t.Fatalf("Failed to create program: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating a program, got %d", resp.StatusCode)
	}
	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	return created["id"].(string)
}

// mondayOnOrBefore snaps a date back to its Monday, which is the only day a
// program may start on. Snapping backwards keeps a past date past; the future
// date it is used on is far enough ahead to stay future.
func mondayOnOrBefore(day time.Time) time.Time {
	return day.AddDate(0, 0, -int((day.Weekday()+6)%7))
}
