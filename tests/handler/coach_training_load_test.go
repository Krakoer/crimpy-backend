package handler_test

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The load view is read against the caller's clock, so the tests place their
// sessions relative to the Monday the request will land in rather than against
// fixed dates that would age out of the window.
const loadTestOffsetMinutes = 120

func loadTestMonday(weeksBack int) time.Time {
	shiftedNow := time.Now().UTC().Add(loadTestOffsetMinutes * time.Minute)
	daysSinceMonday := (int(shiftedNow.Weekday()) + 6) % 7
	monday := shiftedNow.AddDate(0, 0, -daysSinceMonday-7*weeksBack)
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
}

// insertLoadSession writes one session at an instant given on the caller's
// shifted clock, converting it back to the UTC the column stores.
func insertLoadSession(t *testing.T, pool *pgxpool.Pool, userID string, localAt time.Time, activity, durationSeconds int, rpe *int, failed bool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO sessions (user_id, name, notes, date, activity, origin, duration, rpe, rpe_failed)
		 VALUES ($1, 'Load session', '', $2, $3, 'logged', $4, $5, $6)`,
		userID, localAt.Add(-loadTestOffsetMinutes*time.Minute), activity, durationSeconds, rpe, failed)
	if err != nil {
		t.Fatalf("Failed to insert session: %v", err)
	}
}

func rpeOf(value int) *int { return &value }

func setupTrainingLoadApp(t *testing.T) (*fiber.App, *pgxpool.Pool, *db.Queries, string, string, string) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingLoadHandler: handler.NewCoachTrainingLoadHandler(queries, pool),
	})

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "loadcoach-test@example.com")
	userID, _ := testutil.CreateTestUser(t, queries, "loadclient-test@example.com")
	enrollUserDirect(t, pool, coachID, userID)

	return app, pool, queries, coachID, userID, coachToken
}

func fetchTrainingLoad(t *testing.T, app *fiber.App, token, userID string, weeks int) []map[string]interface{} {
	t.Helper()
	url := fmt.Sprintf("/api/coach/clients/%s/training-load?weeks=%d&tz_offset_minutes=%d", userID, weeks, loadTestOffsetMinutes)
	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, url, nil, token), fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Weeks []map[string]interface{} `json:"weeks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}
	return body.Weeks
}

func numberAt(t *testing.T, week map[string]interface{}, key string) float64 {
	t.Helper()
	value, ok := week[key].(float64)
	if !ok {
		t.Fatalf("Expected %s to be a number, got %v", key, week[key])
	}
	return value
}

func TestTrainingLoadAggregatesAWeek(t *testing.T) {
	app, pool, _, _, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	monday := loadTestMonday(0)
	// 90 minutes climbing at 8, 30 minutes hangboard at 6, 60 minutes of
	// workout left unrated, and a failed 45 minute session.
	insertLoadSession(t, pool, userID, monday.Add(18*time.Hour), 1, 5400, rpeOf(8), false)
	insertLoadSession(t, pool, userID, monday.AddDate(0, 0, 2).Add(18*time.Hour), 0, 1800, rpeOf(6), false)
	insertLoadSession(t, pool, userID, monday.AddDate(0, 0, 3).Add(18*time.Hour), 3, 3600, nil, false)
	insertLoadSession(t, pool, userID, monday.AddDate(0, 0, 4).Add(18*time.Hour), 0, 2700, nil, true)

	weeks := fetchTrainingLoad(t, app, coachToken, userID, 4)
	if len(weeks) != 4 {
		t.Fatalf("Expected 4 weeks, got %d", len(weeks))
	}

	current := weeks[3]
	if current["week_start"] != monday.Format(time.DateOnly) {
		t.Fatalf("Expected the last week to start on %s, got %v", monday.Format(time.DateOnly), current["week_start"])
	}
	if got := numberAt(t, current, "session_count"); got != 4 {
		t.Fatalf("Expected 4 sessions, got %v", got)
	}
	if got := numberAt(t, current, "total_minutes"); got != 225 {
		t.Fatalf("Expected 225 total minutes, got %v", got)
	}
	if got := numberAt(t, current, "climbing_minutes"); got != 90 {
		t.Fatalf("Expected 90 climbing minutes, got %v", got)
	}
	// Hangboard and workout are the strength side: 30 + 60 + 45.
	if got := numberAt(t, current, "strength_minutes"); got != 135 {
		t.Fatalf("Expected 135 strength minutes, got %v", got)
	}
	if got := numberAt(t, current, "rated_sessions"); got != 2 {
		t.Fatalf("Expected 2 rated sessions, got %v", got)
	}
	if got := numberAt(t, current, "failed_sessions"); got != 1 {
		t.Fatalf("Expected 1 failed session, got %v", got)
	}
	// The unrated session and the failed one are both outside the mean, so it
	// is 8 and 6 rather than anything dragged towards zero.
	if got := numberAt(t, current, "mean_rpe"); got != 7 {
		t.Fatalf("Expected a mean RPE of 7, got %v", got)
	}
	if got := numberAt(t, current, "acute_load"); got != 7*225 {
		t.Fatalf("Expected an acute load of %v, got %v", 7*225, got)
	}
}

func TestTrainingLoadKeepsAFailedSessionOutOfTheMean(t *testing.T) {
	app, pool, _, _, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	monday := loadTestMonday(0)
	insertLoadSession(t, pool, userID, monday.Add(18*time.Hour), 1, 3600, rpeOf(5), false)
	insertLoadSession(t, pool, userID, monday.AddDate(0, 0, 2).Add(18*time.Hour), 1, 3600, nil, true)

	weeks := fetchTrainingLoad(t, app, coachToken, userID, 1)
	current := weeks[0]

	// A failure is not a 10 and not a zero: the mean is the one rating given.
	if got := numberAt(t, current, "mean_rpe"); got != 5 {
		t.Fatalf("Expected a mean RPE of 5, got %v", got)
	}
	if got := numberAt(t, current, "failed_sessions"); got != 1 {
		t.Fatalf("Expected the failure to be reported on its own, got %v", got)
	}
	// Its minutes still count as training done.
	if got := numberAt(t, current, "total_minutes"); got != 120 {
		t.Fatalf("Expected 120 total minutes, got %v", got)
	}
	if got := numberAt(t, current, "acute_load"); got != 600 {
		t.Fatalf("Expected an acute load of 600, got %v", got)
	}
}

func TestTrainingLoadLeavesAWeekWithNoRatingUnknown(t *testing.T) {
	app, pool, _, _, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	monday := loadTestMonday(0)
	insertLoadSession(t, pool, userID, monday.Add(18*time.Hour), 1, 3600, nil, false)

	current := fetchTrainingLoad(t, app, coachToken, userID, 1)[0]
	if current["mean_rpe"] != nil {
		t.Fatalf("Expected no mean RPE, got %v", current["mean_rpe"])
	}
	// Unknown rather than zero: an hour was trained, the effort is simply not
	// recorded, and answering zero would claim it was free.
	if current["acute_load"] != nil {
		t.Fatalf("Expected an unknown acute load, got %v", current["acute_load"])
	}
	if got := numberAt(t, current, "total_minutes"); got != 60 {
		t.Fatalf("Expected 60 minutes to still be counted, got %v", got)
	}
}

func TestTrainingLoadReturnsEmptyWeeksAsZero(t *testing.T) {
	app, pool, _, _, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	// Two weeks of training with a silent week between them.
	insertLoadSession(t, pool, userID, loadTestMonday(2).Add(18*time.Hour), 1, 3600, rpeOf(6), false)
	insertLoadSession(t, pool, userID, loadTestMonday(0).Add(18*time.Hour), 1, 3600, rpeOf(6), false)

	weeks := fetchTrainingLoad(t, app, coachToken, userID, 3)
	if len(weeks) != 3 {
		t.Fatalf("Expected 3 weeks, got %d", len(weeks))
	}

	middle := weeks[1]
	if middle["week_start"] != loadTestMonday(1).Format(time.DateOnly) {
		t.Fatalf("Expected the silent week to be kept, got %v", middle["week_start"])
	}
	if got := numberAt(t, middle, "session_count"); got != 0 {
		t.Fatalf("Expected no sessions, got %v", got)
	}
	// Zero rather than unknown: nothing was trained, so the load really is zero
	// and the chronic mean has to count it.
	if got := numberAt(t, middle, "acute_load"); got != 0 {
		t.Fatalf("Expected a zero acute load, got %v", got)
	}

	// 360 in the first and last week, nothing in the middle one.
	if got := numberAt(t, weeks[2], "chronic_load"); got != 240 {
		t.Fatalf("Expected a chronic load of 240, got %v", got)
	}
	if got := numberAt(t, weeks[2], "chronic_weeks"); got != 3 {
		t.Fatalf("Expected the chronic mean to rest on 3 weeks, got %v", got)
	}
}

func TestTrainingLoadBuildsTheChronicBaselineFromTheFirstWeekOfHistory(t *testing.T) {
	app, pool, _, _, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	// History starts two weeks ago, inside the window asked for.
	insertLoadSession(t, pool, userID, loadTestMonday(1).Add(18*time.Hour), 1, 3600, rpeOf(6), false)
	insertLoadSession(t, pool, userID, loadTestMonday(0).Add(18*time.Hour), 1, 7200, rpeOf(6), false)

	weeks := fetchTrainingLoad(t, app, coachToken, userID, 4)
	if len(weeks) != 4 {
		t.Fatalf("Expected 4 weeks, got %d", len(weeks))
	}

	// The two weeks before the athlete recorded anything are not silence to be
	// averaged in, so they carry no chronic baseline at all.
	for _, week := range weeks[:2] {
		if week["chronic_load"] != nil {
			t.Fatalf("Expected no chronic load before the history starts, got %v", week["chronic_load"])
		}
		if got := numberAt(t, week, "chronic_weeks"); got != 0 {
			t.Fatalf("Expected no weeks behind the baseline, got %v", got)
		}
	}

	first := weeks[2]
	if got := numberAt(t, first, "chronic_weeks"); got != 1 {
		t.Fatalf("Expected the first week of history to rest on itself, got %v", got)
	}
	if got := numberAt(t, first, "chronic_load"); got != 360 {
		t.Fatalf("Expected a chronic load of 360, got %v", got)
	}
	if got := numberAt(t, first, "acute_chronic_ratio"); got != 1 {
		t.Fatalf("Expected a ratio of exactly 1 in the first week, got %v", got)
	}

	second := weeks[3]
	if got := numberAt(t, second, "chronic_weeks"); got != 2 {
		t.Fatalf("Expected the second week to rest on two, got %v", got)
	}
	if got := numberAt(t, second, "chronic_load"); got != 540 {
		t.Fatalf("Expected a chronic load of 540, got %v", got)
	}
	if got := numberAt(t, second, "load_change_percent"); got != 200 {
		t.Fatalf("Expected the load to have doubled, got %v", got)
	}
}

func TestTrainingLoadCutsTheWeekOnTheCallersMonday(t *testing.T) {
	app, pool, _, _, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	// Monday 00:30 on the caller's clock, which is the Sunday before in UTC.
	// It belongs to the week that Monday opens, not to the one it closes.
	insertLoadSession(t, pool, userID, loadTestMonday(0).Add(30*time.Minute), 1, 3600, rpeOf(6), false)

	weeks := fetchTrainingLoad(t, app, coachToken, userID, 2)
	if got := numberAt(t, weeks[0], "session_count"); got != 0 {
		t.Fatalf("Expected the previous week to be empty, got %v", got)
	}
	if got := numberAt(t, weeks[1], "session_count"); got != 1 {
		t.Fatalf("Expected the session in the current week, got %v", got)
	}
}

func TestTrainingLoadNamesTheProgramWeek(t *testing.T) {
	app, pool, _, coachID, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	start := loadTestMonday(2)
	insertProgram(t, pool, coachID, userID, "Test block", start.Format(time.DateOnly), 8)

	weeks := fetchTrainingLoad(t, app, coachToken, userID, 4)
	if weeks[0]["week_number"] != nil {
		t.Fatalf("Expected no program week before the program starts, got %v", weeks[0]["week_number"])
	}
	if got := numberAt(t, weeks[1], "week_number"); got != 1 {
		t.Fatalf("Expected week 1, got %v", got)
	}
	if got := numberAt(t, weeks[3], "week_number"); got != 3 {
		t.Fatalf("Expected week 3, got %v", got)
	}
	if weeks[3]["program_name"] != "Test block" {
		t.Fatalf("Expected the program name, got %v", weeks[3]["program_name"])
	}
}

func TestTrainingLoadRejectsAClientOfAnotherCoach(t *testing.T) {
	app, pool, queries, _, _, _ := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	_, otherCoachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "loadothercoach-test@example.com")
	strangerID, _ := testutil.CreateTestUser(t, queries, "loadstranger-test@example.com")

	url := fmt.Sprintf("/api/coach/clients/%s/training-load", strangerID)
	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, url, nil, otherCoachToken), fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Expected 403 for a client of another coach, got %d", resp.StatusCode)
	}
}

func TestTrainingLoadRejectsInvalidParameters(t *testing.T) {
	app, pool, _, _, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	for _, query := range []string{"?weeks=0", "?weeks=53", "?weeks=many", "?tz_offset_minutes=9999"} {
		url := fmt.Sprintf("/api/coach/clients/%s/training-load%s", userID, query)
		resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, url, nil, coachToken), fiber.TestConfig{Timeout: 10 * time.Second})
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Expected 400 for %s, got %d", query, resp.StatusCode)
		}
	}
}

func TestTrainingLoadDefaultsToTwelveWeeks(t *testing.T) {
	app, pool, _, _, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	url := fmt.Sprintf("/api/coach/clients/%s/training-load", userID)
	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, url, nil, coachToken), fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Weeks []map[string]interface{} `json:"weeks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}
	if len(body.Weeks) != 12 {
		t.Fatalf("Expected 12 weeks by default, got %d", len(body.Weeks))
	}
	// An athlete who recorded nothing still gets a full grid of zero weeks.
	if got := numberAt(t, body.Weeks[0], "acute_load"); got != 0 {
		t.Fatalf("Expected a zero acute load, got %v", got)
	}
	if body.Weeks[0]["chronic_load"] != nil {
		t.Fatalf("Expected no chronic baseline with no history at all, got %v", body.Weeks[0]["chronic_load"])
	}
}
