package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// weekNameFixture builds a coach, an athlete enrolled with them and a program
// with one training to hang the weeks off, which every test here needs before
// it can say anything about a name.
type weekNameFixture struct {
	app        *fiber.App
	coachToken string
	userID     string
	userToken  string
	programID  string
	trainingID string
}

func setupWeekNameFixture(t *testing.T, emailPrefix string) weekNameFixture {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, emailPrefix+"coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, emailPrefix+"user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	return weekNameFixture{
		app:        app,
		coachToken: coachToken,
		userID:     userID,
		userToken:  userToken,
		programID:  programID,
		trainingID: trainingID,
	}
}

// saveWeek upserts week 1 with the given payload fields, always carrying one
// session so the week is a week a coach could really have prescribed.
func (f weekNameFixture) saveWeek(t *testing.T, fields map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	payload := map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": f.trainingID, "day_of_week": 0},
		},
	}
	for k, v := range fields {
		payload[k] = v
	}
	body, _ := json.Marshal(payload)
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(f.userID, f.programID, 1), body, f.coachToken)
	resp, err := f.app.Test(req)
	if err != nil {
		t.Fatalf("Week save request failed: %v", err)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func (f weekNameFixture) getJSON(t *testing.T, url, token string) map[string]interface{} {
	t.Helper()
	resp, err := f.app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, url, nil, token))
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 from %s, got %d", url, resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func (f weekNameFixture) getList(t *testing.T, url, token string) []interface{} {
	t.Helper()
	resp, err := f.app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, url, nil, token))
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 from %s, got %d", url, resp.StatusCode)
	}
	var result []interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

// The name has to reach both audiences: the coach who wrote it, on the detail
// read and on the summary list the program page scans, and the athlete, on the
// same two reads of their own program. A name stored but missing from one of
// the four is a name nobody can rely on.
func TestWeekName_ReachesEveryReadPath(t *testing.T) {
	f := setupWeekNameFixture(t, "wkname1")

	status, saved := f.saveWeek(t, map[string]interface{}{"name": "capacity"})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 saving a named week, got %d: %v", status, saved)
	}
	if saved["name"] != "capacity" {
		t.Errorf("Expected the save echo to carry the name, got %v", saved["name"])
	}

	coachWeek := f.getJSON(t, weekURL(f.userID, f.programID, 1), f.coachToken)
	if coachWeek["name"] != "capacity" {
		t.Errorf("Expected the coach week read to carry the name, got %v", coachWeek["name"])
	}

	coachList := f.getList(t, weeksURL(f.userID, f.programID), f.coachToken)
	if len(coachList) != 1 {
		t.Fatalf("Expected 1 week in the coach list, got %d", len(coachList))
	}
	if coachList[0].(map[string]interface{})["name"] != "capacity" {
		t.Errorf("Expected the coach week list to carry the name, got %v", coachList[0])
	}

	myWeek := f.getJSON(t, fmt.Sprintf("/api/user/programs/%s/weeks/1", f.programID), f.userToken)
	if myWeek["name"] != "capacity" {
		t.Errorf("Expected the athlete week read to carry the name, got %v", myWeek["name"])
	}

	myList := f.getList(t, fmt.Sprintf("/api/user/programs/%s/weeks", f.programID), f.userToken)
	if len(myList) != 1 {
		t.Fatalf("Expected 1 week in the athlete list, got %d", len(myList))
	}
	if myList[0].(map[string]interface{})["name"] != "capacity" {
		t.Errorf("Expected the athlete week list to carry the name, got %v", myList[0])
	}
}

// The name and the notes are different things said about the same week, so
// neither write path may stand in for the other: a week carrying both must
// hand both back, and one carrying only a name must not blank a note.
func TestWeekName_IsIndependentOfNotes(t *testing.T) {
	f := setupWeekNameFixture(t, "wkname2")

	status, saved := f.saveWeek(t, map[string]interface{}{
		"name":  "deload",
		"notes": "you said you were travelling Thursday",
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d: %v", status, saved)
	}
	if saved["name"] != "deload" {
		t.Errorf("Expected name deload, got %v", saved["name"])
	}
	if saved["notes"] != "you said you were travelling Thursday" {
		t.Errorf("Expected the notes to survive beside the name, got %v", saved["notes"])
	}
}

// A week saved without a name loses the one it had, the way it loses notes:
// the upsert replaces the week with what the payload says it is, and the
// editor sends the whole week every time.
func TestWeekName_ClearedByASaveThatOmitsIt(t *testing.T) {
	f := setupWeekNameFixture(t, "wkname3")

	if status, saved := f.saveWeek(t, map[string]interface{}{"name": "max strength"}); status != fiber.StatusOK {
		t.Fatalf("Expected 200 on the first save, got %d: %v", status, saved)
	}

	status, saved := f.saveWeek(t, nil)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 on the second save, got %d: %v", status, saved)
	}
	if _, present := saved["name"]; present {
		t.Errorf("Expected no name after a save that omitted it, got %v", saved["name"])
	}

	coachWeek := f.getJSON(t, weekURL(f.userID, f.programID, 1), f.coachToken)
	if _, present := coachWeek["name"]; present {
		t.Errorf("Expected the cleared name to stay cleared on read, got %v", coachWeek["name"])
	}
}

// A name of nothing but spaces reads as a set name in every list that shows
// it while holding nothing, so it is stored as no name rather than refused:
// the coach clearing the field means the same thing as never filling it.
func TestWeekName_BlankIsStoredAsAbsent(t *testing.T) {
	f := setupWeekNameFixture(t, "wkname4")

	status, saved := f.saveWeek(t, map[string]interface{}{"name": "   "})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 for a blank name, got %d: %v", status, saved)
	}
	if _, present := saved["name"]; present {
		t.Errorf("Expected a blank name to come back absent, got %v", saved["name"])
	}
}

// Surrounding whitespace is not part of the label, and leaving it in would
// make two names that read alike sort and group apart.
func TestWeekName_IsTrimmed(t *testing.T) {
	f := setupWeekNameFixture(t, "wkname5")

	status, saved := f.saveWeek(t, map[string]interface{}{"name": "  tests  "})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d: %v", status, saved)
	}
	if saved["name"] != "tests" {
		t.Errorf("Expected the name trimmed to tests, got %v", saved["name"])
	}
}

// Refused rather than cut, like the item comment and goal: a coach only finds
// out about a silent truncation once the athlete reads half of it.
func TestWeekName_RejectsOverLength(t *testing.T) {
	f := setupWeekNameFixture(t, "wkname6")

	status, result := f.saveWeek(t, map[string]interface{}{"name": strings.Repeat("a", 61)})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a name over the limit, got %d: %v", status, result)
	}
	errText, _ := result["error"].(string)
	if !strings.Contains(errText, "60") {
		t.Errorf("Expected the refusal to name the limit, got %v", result["error"])
	}
}

// The limit is counted in runes, so a name written in the accented French a
// coach actually writes gets the same 60 characters as one written in ASCII,
// and the boundary itself is accepted rather than refused.
func TestWeekName_AllowsExactlyTheLimitInMultibyteRunes(t *testing.T) {
	f := setupWeekNameFixture(t, "wkname7")

	name := strings.Repeat("e", 60)
	status, saved := f.saveWeek(t, map[string]interface{}{"name": name})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 for a name at exactly the limit, got %d: %v", status, saved)
	}
	if saved["name"] != name {
		t.Errorf("Expected the name to round trip in full, got %v", saved["name"])
	}

	accented := strings.Repeat("é", 60)
	status, saved = f.saveWeek(t, map[string]interface{}{"name": accented})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 for 60 accented runes, got %d: %v", status, saved)
	}
	if saved["name"] != accented {
		t.Errorf("Expected the accented name to round trip in full, got %v", saved["name"])
	}
}

// The whole point of the field is scanning the phase arc down a program, so
// the summary list has to hand back what each week is called, not only the
// first one's.
func TestWeekName_ListsThePhaseArcOfTheProgram(t *testing.T) {
	f := setupWeekNameFixture(t, "wkname8")

	phases := map[int]string{1: "familiarisation", 2: "physical tests", 3: "capacity"}
	for week, phase := range phases {
		body, _ := json.Marshal(map[string]interface{}{
			"name": phase,
			"sessions": []map[string]interface{}{
				{"training_id": f.trainingID, "day_of_week": 0},
			},
		})
		req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(f.userID, f.programID, week), body, f.coachToken)
		resp, err := f.app.Test(req)
		if err != nil {
			t.Fatalf("Week %d save failed: %v", week, err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("Expected 200 saving week %d, got %d", week, resp.StatusCode)
		}
	}

	list := f.getList(t, weeksURL(f.userID, f.programID), f.coachToken)
	if len(list) != len(phases) {
		t.Fatalf("Expected %d weeks, got %d", len(phases), len(list))
	}
	for _, entry := range list {
		week := entry.(map[string]interface{})
		number := int(week["week_number"].(float64))
		if week["name"] != phases[number] {
			t.Errorf("Expected week %d named %s, got %v", number, phases[number], week["name"])
		}
	}
}
