package handler_test

import (
	"context"
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

func insertSession(t *testing.T, pool *pgxpool.Pool, userID, name, notes string, date time.Time, reply *string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO sessions (user_id, name, notes, date, activity, origin, coach_reply, coach_reply_at)
		 VALUES ($1, $2, $3, $4, 1, 'logged', $5, CASE WHEN $5::text IS NULL THEN NULL ELSE now() END)
		 RETURNING id`,
		userID, name, notes, date, reply).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert session: %v", err)
	}
	return id
}

func insertDeclaredWeek(t *testing.T, pool *pgxpool.Pool, userID, weekStart string) {
	t.Helper()
	for day := 0; day < 7; day++ {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO coachee_day_availabilities (user_id, week_start, day_of_week, is_available)
			 VALUES ($1, $2, $3, $4)`,
			userID, weekStart, day, day%2 == 0)
		if err != nil {
			t.Fatalf("Failed to declare availability: %v", err)
		}
	}
}

func insertProgram(t *testing.T, pool *pgxpool.Pool, coachID, userID, name, startDate string, durationWeeks int) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO coach_programs (coach_id, user_id, name, start_date, duration_weeks)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		coachID, userID, name, startDate, durationWeeks).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert program: %v", err)
	}
	return id
}

// programWeekWithSession fills one week of a program, which is what takes it out
// of the empty week list.
func programWeekWithSession(t *testing.T, pool *pgxpool.Pool, coachID, programID string, weekNumber int) {
	t.Helper()
	ctx := context.Background()
	var trainingID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO trainings (user_id, title) VALUES ($1, 'Todo test training') RETURNING id`,
		coachID).Scan(&trainingID); err != nil {
		t.Fatalf("Failed to insert training: %v", err)
	}
	var weekID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO coach_program_weeks (program_id, week_number) VALUES ($1, $2) RETURNING id`,
		programID, weekNumber).Scan(&weekID); err != nil {
		t.Fatalf("Failed to insert program week: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO coach_program_week_sessions (week_id, training_id, day_of_week) VALUES ($1, $2, 0)`,
		weekID, trainingID); err != nil {
		t.Fatalf("Failed to insert program week session: %v", err)
	}
}

func mondayOfTestWeek(now time.Time, weeksAhead int) string {
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	monday := now.AddDate(0, 0, -daysSinceMonday+7*weeksAhead)
	return monday.Format(time.DateOnly)
}

// momentStillAhead names a weekly moment strictly later in the week than now,
// so the empty week check reads as not reached. There is no such moment in the
// last minute of a week, which is the only time the caller has to skip.
func momentStillAhead(now time.Time) (day, hour, minute int, ok bool) {
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	if daysSinceMonday < 6 {
		return daysSinceMonday + 1, 0, 0, true
	}
	if now.Hour() < 23 {
		return 6, now.Hour() + 1, 0, true
	}
	if now.Minute() < 59 {
		return 6, 23, now.Minute() + 1, true
	}
	return 0, 0, 0, false
}

func parseInstant(t *testing.T, raw string) time.Time {
	t.Helper()
	instant, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("Failed to parse %q: %v", raw, err)
	}
	return instant
}

func occurredAt(t *testing.T, event map[string]interface{}) time.Time {
	t.Helper()
	return parseInstant(t, event["occurred_at"].(string))
}

func getFeed(t *testing.T, app *fiber.App, token, query string) []map[string]interface{} {
	t.Helper()
	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/feed"+query, nil, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to get the feed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 getting the feed, got %d", resp.StatusCode)
	}
	var events []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&events)
	return events
}

func getTodo(t *testing.T, app *fiber.App, token string) map[string]interface{} {
	t.Helper()
	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/todo", nil, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to get TODO: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 getting the TODO, got %d", resp.StatusCode)
	}
	var todo map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&todo)
	return todo
}

func putTodoSettings(t *testing.T, app *fiber.App, token string, day, hour, minute int) *http.Response {
	t.Helper()
	payload, _ := json.Marshal(map[string]interface{}{
		"empty_week_day_of_week": day,
		"empty_week_hour":        hour,
		"empty_week_minute":      minute,
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/coach/todo-settings", payload, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to put TODO settings: %v", err)
	}
	return resp
}

func TestCoachFeed_ReportsWhatCoacheesDid(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "feedcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "feedathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	now := time.Now().UTC()
	insertSession(t, pool, userID, "Endurance 4x4", "shoulder felt tight", now.Add(-2*time.Hour), nil)
	insertDeclaredWeek(t, pool, userID, mondayOfTestWeek(now, 1))

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/feed", nil, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to get the feed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 getting the feed, got %d", resp.StatusCode)
	}

	var events []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&events)

	kinds := map[string]map[string]interface{}{}
	for _, event := range events {
		kinds[event["kind"].(string)] = event
	}
	for _, kind := range []string{"session_completed", "availability_declared", "coachee_enrolled"} {
		if _, present := kinds[kind]; !present {
			t.Fatalf("Expected a %s event in the feed, got %v", kind, events)
		}
	}

	session := kinds["session_completed"]
	if session["title"] != "Endurance 4x4" {
		t.Errorf("Expected the session name as the title, got %v", session["title"])
	}
	if session["note"] != "shoulder felt tight" {
		t.Errorf("Expected the athlete's note on the event, got %v", session["note"])
	}
	if session["user_firstname"] == nil || session["user_id"] != userID {
		t.Errorf("Expected the event to name the coachee, got %v", session)
	}
	if kinds["availability_declared"]["week_start"] != mondayOfTestWeek(now, 1) {
		t.Errorf("Expected the declared week on the event, got %v", kinds["availability_declared"]["week_start"])
	}

	// Newest first, which is what makes the before cursor a page boundary.
	for i := 1; i < len(events); i++ {
		if occurredAt(t, events[i-1]).Before(occurredAt(t, events[i])) {
			t.Fatalf("Expected the feed ordered newest first, got %v", events)
		}
	}
}

func TestCoachFeed_HonoursLimitAndBefore(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "feedpagecoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "feedpageathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	now := time.Now().UTC()
	for i := 1; i <= 3; i++ {
		insertSession(t, pool, userID, fmt.Sprintf("Session %d", i), "", now.Add(-time.Duration(i)*time.Hour), nil)
	}

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/feed?limit=2", nil, coachToken)
	resp, _ := app.Test(req)
	var page []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&page)
	if len(page) != 2 {
		t.Fatalf("Expected 2 events for limit=2, got %d", len(page))
	}
	// The enrollment is itself the newest event, so the first page holds it and
	// the session played an hour ago.
	if page[0]["kind"] != "coachee_enrolled" || page[1]["title"] != "Session 1" {
		t.Fatalf("Expected the enrollment then the newest session, got %v", page)
	}

	cursor := page[1]["occurred_at"].(string)
	req = testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/feed?limit=2&before="+cursor, nil, coachToken)
	resp, _ = app.Test(req)
	var next []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&next)
	for _, event := range next {
		if !occurredAt(t, event).Before(parseInstant(t, cursor)) {
			t.Errorf("Expected every event under the cursor, got %v", event["occurred_at"])
		}
	}
	if len(next) != 2 || next[0]["title"] != "Session 2" || next[1]["title"] != "Session 3" {
		t.Fatalf("Expected the next page to carry the two older sessions, got %v", next)
	}

	req = testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/feed?before=not-a-date", nil, coachToken)
	resp, _ = app.Test(req)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for an unparseable before cursor, got %d", resp.StatusCode)
	}
}

func TestCoachFeed_DoesNotLeakAnotherCoachsCoachees(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, _ := testutil.CreateTestValidatedCoachUser(t, pool, queries, "feedowncoach@test.com")
	otherCoachID, otherCoachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "feedothercoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "feedprivateathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	otherUserID, _ := testutil.CreateTestUser(t, queries, "feedotherathlete@test.com")
	enrollUserDirect(t, pool, otherCoachID, otherUserID)

	now := time.Now().UTC()
	insertSession(t, pool, userID, "Private session", "private note", now, nil)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/feed", nil, otherCoachToken)
	resp, _ := app.Test(req)
	var events []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&events)
	for _, event := range events {
		if event["user_id"] == userID {
			t.Fatalf("Another coach's coachee leaked into the feed: %v", event)
		}
	}
}

func TestCoachFeed_RefusesANonCoach(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "feednotacoach@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	for _, url := range []string{"/api/coach/feed", "/api/coach/todo", "/api/coach/todo-settings"} {
		req := testutil.NewRequestWithAuth(http.MethodGet, url, nil, userToken)
		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusForbidden {
			t.Errorf("Expected 403 on %s for a non coach, got %d", url, resp.StatusCode)
		}
	}
}

func TestCoachTodo_ListsOnlyUnansweredFeedback(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "todocoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "todoathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	answered := "keep the elbow in"
	now := time.Now().UTC()
	insertSession(t, pool, userID, "Waiting on you", "the last set was brutal", now, nil)
	insertSession(t, pool, userID, "Already answered", "felt good", now, &answered)
	insertSession(t, pool, userID, "Nothing to say", "", now, nil)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	todo := getTodo(t, app, coachToken)
	pending := todo["pending_feedback"].([]interface{})
	if len(pending) != 1 {
		t.Fatalf("Expected exactly the unanswered session, got %v", pending)
	}
	if todo["pending_feedback_total"].(float64) != 1 {
		t.Errorf("Expected the one unanswered session counted in all, got %v", todo["pending_feedback_total"])
	}
	if todo["sessions_this_week"].(float64) != 3 {
		t.Errorf("Expected the three sessions of this week counted, got %v", todo["sessions_this_week"])
	}
	item := pending[0].(map[string]interface{})
	if item["session_name"] != "Waiting on you" {
		t.Errorf("Expected the unanswered session, got %v", item["session_name"])
	}
	if item["notes"] != "the last set was brutal" {
		t.Errorf("Expected the athlete's notes on the item, got %v", item["notes"])
	}
	if item["user_id"] != userID {
		t.Errorf("Expected the item to name the coachee, got %v", item["user_id"])
	}
}

func TestCoachTodo_DoesNotLeakAnotherCoachsFeedback(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, _ := testutil.CreateTestValidatedCoachUser(t, pool, queries, "todoowncoach@test.com")
	_, otherCoachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "todoothercoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "todoprivateathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	insertSession(t, pool, userID, "Private session", "private note", time.Now().UTC(), nil)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	todo := getTodo(t, app, otherCoachToken)
	if len(todo["pending_feedback"].([]interface{})) != 0 {
		t.Fatalf("Another coach's unanswered feedback leaked: %v", todo["pending_feedback"])
	}
}

func TestCoachTodo_EmptyWeeksWaitForTheConfiguredMoment(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "emptyweekcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "emptyweekathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	now := time.Now().UTC()
	nextMonday := mondayOfTestWeek(now, 1)
	insertProgram(t, pool, coachID, userID, "Unprogrammed block", nextMonday, 4)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	day, hour, minute, ok := momentStillAhead(now)
	if !ok {
		t.Skip("no moment left in this week to schedule the check at")
	}
	if resp := putTodoSettings(t, app, coachToken, day, hour, minute); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 saving the settings, got %d", resp.StatusCode)
	}

	todo := getTodo(t, app, coachToken)
	check := todo["empty_week_check"].(map[string]interface{})
	if check["reached"] != false {
		t.Errorf("Expected the check not reached before the moment, got %v", check["reached"])
	}
	if len(todo["empty_weeks"].([]interface{})) != 0 {
		t.Fatalf("Expected no empty week before the moment, got %v", todo["empty_weeks"])
	}

	// Monday at midnight has always passed by the time a week is being read.
	if resp := putTodoSettings(t, app, coachToken, 0, 0, 0); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 saving the settings, got %d", resp.StatusCode)
	}

	todo = getTodo(t, app, coachToken)
	check = todo["empty_week_check"].(map[string]interface{})
	if check["reached"] != true {
		t.Fatalf("Expected the check reached, got %v", check["reached"])
	}
	if check["week_start"] != nextMonday {
		t.Errorf("Expected the check to name next Monday, got %v", check["week_start"])
	}
	empty := todo["empty_weeks"].([]interface{})
	if len(empty) != 1 {
		t.Fatalf("Expected the unprogrammed week, got %v", empty)
	}
	item := empty[0].(map[string]interface{})
	if item["program_name"] != "Unprogrammed block" {
		t.Errorf("Expected the program named, got %v", item["program_name"])
	}
	if item["week_number"].(float64) != 1 {
		t.Errorf("Expected week 1 of a program starting next Monday, got %v", item["week_number"])
	}
	if item["week_start"] != nextMonday {
		t.Errorf("Expected the item to name next Monday, got %v", item["week_start"])
	}
	if item["scope"] != "next" {
		t.Errorf("Expected the item scoped to the next week, got %v", item["scope"])
	}
}

func TestCoachTodo_SkipsProgrammedAndFinishedWeeks(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "filledweekcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "filledweekathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	now := time.Now().UTC()
	nextMonday := mondayOfTestWeek(now, 1)
	programmed := insertProgram(t, pool, coachID, userID, "Programmed block", nextMonday, 4)
	programWeekWithSession(t, pool, coachID, programmed, 1)

	// One week long and starting last Monday, so next week is past its end.
	insertProgram(t, pool, coachID, userID, "Finished block", mondayOfTestWeek(now, -1), 1)
	// Starts the week after next, so next week is before its start.
	insertProgram(t, pool, coachID, userID, "Future block", mondayOfTestWeek(now, 2), 4)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	if resp := putTodoSettings(t, app, coachToken, 0, 0, 0); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 saving the settings, got %d", resp.StatusCode)
	}

	todo := getTodo(t, app, coachToken)
	if len(todo["empty_weeks"].([]interface{})) != 0 {
		t.Fatalf("Expected no empty week, got %v", todo["empty_weeks"])
	}
}

func TestCoachTodo_ListsAnEmptyCurrentWeekBeforeTheMoment(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "currentweekcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "currentweekathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	now := time.Now().UTC()
	thisMonday := mondayOfTestWeek(now, 0)
	insertProgram(t, pool, coachID, userID, "Running block", thisMonday, 4)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	day, hour, minute, ok := momentStillAhead(now)
	if !ok {
		t.Skip("no moment left in this week to schedule the check at")
	}
	if resp := putTodoSettings(t, app, coachToken, day, hour, minute); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 saving the settings, got %d", resp.StatusCode)
	}

	todo := getTodo(t, app, coachToken)
	if reached := todo["empty_week_check"].(map[string]interface{})["reached"]; reached != false {
		t.Fatalf("Expected the check not reached before the moment, got %v", reached)
	}

	// The week the athlete is training now is not the moment's to hold back.
	empty := todo["empty_weeks"].([]interface{})
	if len(empty) != 1 {
		t.Fatalf("Expected the unprogrammed current week, got %v", empty)
	}
	item := empty[0].(map[string]interface{})
	if item["scope"] != "current" {
		t.Errorf("Expected the item scoped to the current week, got %v", item["scope"])
	}
	if item["program_name"] != "Running block" {
		t.Errorf("Expected the program named, got %v", item["program_name"])
	}
	if item["week_number"].(float64) != 1 {
		t.Errorf("Expected week 1 of a program starting this Monday, got %v", item["week_number"])
	}
	if item["week_start"] != thisMonday {
		t.Errorf("Expected the item to name this Monday, got %v", item["week_start"])
	}
}

func TestCoachTodo_ListsTheCurrentWeekBeforeTheNextOne(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "bothweekscoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "bothweeksathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	now := time.Now().UTC()
	thisMonday := mondayOfTestWeek(now, 0)
	nextMonday := mondayOfTestWeek(now, 1)
	insertProgram(t, pool, coachID, userID, "Wide open block", thisMonday, 4)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})
	if resp := putTodoSettings(t, app, coachToken, 0, 0, 0); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 saving the settings, got %d", resp.StatusCode)
	}

	empty := getTodo(t, app, coachToken)["empty_weeks"].([]interface{})
	if len(empty) != 2 {
		t.Fatalf("Expected both unprogrammed weeks, got %v", empty)
	}
	current := empty[0].(map[string]interface{})
	next := empty[1].(map[string]interface{})
	if current["scope"] != "current" || current["week_start"] != thisMonday || current["week_number"].(float64) != 1 {
		t.Errorf("Expected the current week first, got %v", current)
	}
	if next["scope"] != "next" || next["week_start"] != nextMonday || next["week_number"].(float64) != 2 {
		t.Errorf("Expected the next week second, got %v", next)
	}
}

func TestCoachTodo_SkipsAProgrammedCurrentWeek(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "filledcurrentcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "filledcurrentathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	now := time.Now().UTC()
	thisMonday := mondayOfTestWeek(now, 0)
	program := insertProgram(t, pool, coachID, userID, "Half written block", thisMonday, 4)
	programWeekWithSession(t, pool, coachID, program, 1)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})
	if resp := putTodoSettings(t, app, coachToken, 0, 0, 0); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 saving the settings, got %d", resp.StatusCode)
	}

	empty := getTodo(t, app, coachToken)["empty_weeks"].([]interface{})
	if len(empty) != 1 {
		t.Fatalf("Expected only the unprogrammed next week, got %v", empty)
	}
	if scope := empty[0].(map[string]interface{})["scope"]; scope != "next" {
		t.Errorf("Expected the programmed current week left out, got an item scoped %v", scope)
	}
}

func TestCoachTodoSettings_DefaultsThenSaves(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "todosettingscoach@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/todo-settings", nil, coachToken)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 reading settings never saved, got %d", resp.StatusCode)
	}
	var settings map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&settings)
	if settings["empty_week_day_of_week"].(float64) != 4 || settings["empty_week_hour"].(float64) != 21 {
		t.Errorf("Expected the Friday 21:00 default, got %v", settings)
	}

	if resp := putTodoSettings(t, app, coachToken, 2, 8, 30); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 saving the settings, got %d", resp.StatusCode)
	}

	req = testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/todo-settings", nil, coachToken)
	resp, _ = app.Test(req)
	json.NewDecoder(resp.Body).Decode(&settings)
	if settings["empty_week_day_of_week"].(float64) != 2 || settings["empty_week_hour"].(float64) != 8 || settings["empty_week_minute"].(float64) != 30 {
		t.Errorf("Expected the saved settings back, got %v", settings)
	}
}

func TestCoachTodoSettings_RejectsAMomentOutsideTheWeek(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "todobadsettings@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	for _, bad := range [][3]int{{7, 21, 0}, {-1, 21, 0}, {4, 24, 0}, {4, 21, 60}} {
		if resp := putTodoSettings(t, app, coachToken, bad[0], bad[1], bad[2]); resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for %v, got %d", bad, resp.StatusCode)
		}
	}
}

func TestCoachTodo_RejectsAnImpossibleTimezoneOffset(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "todotzcoach@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/todo?tz_offset_minutes=5000", nil, coachToken)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for an offset no timezone has, got %d", resp.StatusCode)
	}
}

func TestCoachFeed_PagesThroughEventsSharingASecond(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "feedsubsecondcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "feedsubsecondathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	// Three sessions inside one second. A cursor formatted to whole seconds
	// would read as the start of that second and skip the two under it.
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	insertSession(t, pool, userID, "Third", "", base.Add(300*time.Millisecond), nil)
	insertSession(t, pool, userID, "Second", "", base.Add(600*time.Millisecond), nil)
	insertSession(t, pool, userID, "First", "", base.Add(900*time.Millisecond), nil)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	page := getFeed(t, app, coachToken, "?limit=1&before="+url.QueryEscape(base.Add(time.Second).Format(time.RFC3339Nano)))
	if len(page) != 1 || page[0]["title"] != "First" {
		t.Fatalf("Expected the newest of the three, got %v", page)
	}

	seen := []string{page[0]["title"].(string)}
	for range 2 {
		cursor := page[len(page)-1]["occurred_at"].(string)
		page = getFeed(t, app, coachToken, "?limit=1&before="+url.QueryEscape(cursor))
		if len(page) != 1 {
			t.Fatalf("Expected the paging to reach every session, stopped after %v", seen)
		}
		seen = append(seen, page[0]["title"].(string))
	}

	if seen[0] != "First" || seen[1] != "Second" || seen[2] != "Third" {
		t.Errorf("Expected the three sessions in order, got %v", seen)
	}
}

func TestCoachTodo_DropsTheProgramsOfAnUnenrolledAthlete(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "unenrolledcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "unenrolledathlete@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	now := time.Now().UTC()
	insertProgram(t, pool, coachID, userID, "Abandoned block", mondayOfTestWeek(now, 1), 12)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})
	if resp := putTodoSettings(t, app, coachToken, 0, 0, 0); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 saving the settings, got %d", resp.StatusCode)
	}

	if len(getTodo(t, app, coachToken)["empty_weeks"].([]interface{})) != 1 {
		t.Fatalf("Expected the empty week while the athlete is enrolled")
	}

	// The program outlives the enrollment, and every route that could open or
	// delete it answers 403 once the athlete has left, so a TODO item for it
	// would be one the coach can never clear.
	if _, err := pool.Exec(context.Background(),
		"DELETE FROM coach_enrollments WHERE coach_id = $1 AND user_id = $2", coachID, userID); err != nil {
		t.Fatalf("Failed to unenroll: %v", err)
	}

	if empty := getTodo(t, app, coachToken)["empty_weeks"].([]interface{}); len(empty) != 0 {
		t.Fatalf("Expected no empty week for an athlete who left, got %v", empty)
	}
}

func TestCoachTodoSettings_RefusesANonCoachWrite(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "todowritenotacoach@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTodoHandler: handler.NewCoachTodoHandler(queries, pool),
	})

	if resp := putTodoSettings(t, app, userToken, 4, 21, 0); resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403 writing TODO settings as a non coach, got %d", resp.StatusCode)
	}
}
