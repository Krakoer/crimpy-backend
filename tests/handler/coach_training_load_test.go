package handler_test

import (
	"context"
	"crimpy/backend/internal/db"
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
func insertLoadSessionAt(t *testing.T, pool *pgxpool.Pool, userID string, localAt time.Time, offsetMinutes int, activity, durationSeconds int, rpe *int, failed bool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO sessions (user_id, name, notes, date, activity, origin, duration, rpe, rpe_failed)
		 VALUES ($1, 'Load session', '', $2, $3, 'logged', $4, $5, $6)`,
		userID, localAt.Add(-time.Duration(offsetMinutes)*time.Minute), activity, durationSeconds, rpe, failed)
	if err != nil {
		t.Fatalf("Failed to insert session: %v", err)
	}
}

func insertLoadSession(t *testing.T, pool *pgxpool.Pool, userID string, localAt time.Time, activity, durationSeconds int, rpe *int, failed bool) {
	t.Helper()
	insertLoadSessionAt(t, pool, userID, localAt, loadTestOffsetMinutes, activity, durationSeconds, rpe, failed)
}

// insertLoadSessionInstant writes one session at a real instant, rather than at
// a wall clock time on a shifted clock, which is what a test about zones needs.
func insertLoadSessionInstant(t *testing.T, pool *pgxpool.Pool, userID string, at time.Time, activity, durationSeconds int, rpe *int) {
	t.Helper()
	insertLoadSessionAt(t, pool, userID, at, 0, activity, durationSeconds, rpe, false)
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
	// workout left unrated, a failed 45 minute session, and 30 minutes of
	// stretching at 7, which belongs to the total and to neither side of the
	// climbing to strength split.
	insertLoadSession(t, pool, userID, monday.Add(18*time.Hour), 1, 5400, rpeOf(8), false)
	insertLoadSession(t, pool, userID, monday.AddDate(0, 0, 2).Add(18*time.Hour), 0, 1800, rpeOf(6), false)
	insertLoadSession(t, pool, userID, monday.AddDate(0, 0, 3).Add(18*time.Hour), 3, 3600, nil, false)
	insertLoadSession(t, pool, userID, monday.AddDate(0, 0, 4).Add(18*time.Hour), 0, 2700, nil, true)
	insertLoadSession(t, pool, userID, monday.AddDate(0, 0, 5).Add(18*time.Hour), 2, 1800, rpeOf(7), false)

	weeks := fetchTrainingLoad(t, app, coachToken, userID, 4)
	if len(weeks) != 4 {
		t.Fatalf("Expected 4 weeks, got %d", len(weeks))
	}

	current := weeks[3]
	if current["week_start"] != monday.Format(time.DateOnly) {
		t.Fatalf("Expected the last week to start on %s, got %v", monday.Format(time.DateOnly), current["week_start"])
	}
	if got := numberAt(t, current, "session_count"); got != 5 {
		t.Fatalf("Expected 5 sessions, got %v", got)
	}
	if got := numberAt(t, current, "total_minutes"); got != 255 {
		t.Fatalf("Expected 255 total minutes, got %v", got)
	}
	if got := numberAt(t, current, "climbing_minutes"); got != 90 {
		t.Fatalf("Expected 90 climbing minutes, got %v", got)
	}
	// Hangboard and workout are the strength side: 30 + 60 + 45.
	if got := numberAt(t, current, "strength_minutes"); got != 135 {
		t.Fatalf("Expected 135 strength minutes, got %v", got)
	}
	// Stretching is neither side. It has to stay in the total and out of both
	// buckets, or widening either filter would leave the whole suite green
	// while the ratio the coach reads moves.
	total := numberAt(t, current, "total_minutes")
	split := numberAt(t, current, "climbing_minutes") + numberAt(t, current, "strength_minutes")
	if total-split != 30 {
		t.Fatalf("Expected the 30 stretching minutes to sit outside the split, got %v", total-split)
	}
	if got := numberAt(t, current, "rated_sessions"); got != 3 {
		t.Fatalf("Expected 3 rated sessions, got %v", got)
	}
	if got := numberAt(t, current, "failed_sessions"); got != 1 {
		t.Fatalf("Expected 1 failed session, got %v", got)
	}
	// The unrated session and the failed one are both outside the mean, so it
	// is 8, 6 and 7 rather than anything dragged towards zero.
	if got := numberAt(t, current, "mean_rpe"); got != 7 {
		t.Fatalf("Expected a mean RPE of 7, got %v", got)
	}
	if got := numberAt(t, current, "acute_load"); got != 7*255 {
		t.Fatalf("Expected an acute load of %v, got %v", 7*255, got)
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

// A week the athlete recorded sessions in but gave no duration to is not a week
// they trained for free. duration is NOT NULL DEFAULT 0, so a client that omits
// it writes a zero that means "not recorded", and multiplying the mean RPE by it
// would hand the coach a load of zero for a week that was trained.
func TestTrainingLoadLeavesAWeekWithNoDurationUnknown(t *testing.T) {
	app, pool, _, _, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	monday := loadTestMonday(0)
	insertLoadSession(t, pool, userID, monday.Add(18*time.Hour), 1, 0, rpeOf(8), false)

	current := fetchTrainingLoad(t, app, coachToken, userID, 1)[0]
	if got := numberAt(t, current, "session_count"); got != 1 {
		t.Fatalf("Expected the session to be counted, got %v", got)
	}
	if got := numberAt(t, current, "mean_rpe"); got != 8 {
		t.Fatalf("Expected the rating to stand on its own, got %v", got)
	}
	if current["acute_load"] != nil {
		t.Fatalf("Expected an unknown acute load, got %v", current["acute_load"])
	}
	// And it must not enter the chronic mean as a measured rest week.
	if current["chronic_load"] != nil {
		t.Fatalf("Expected no baseline from a week with no knowable load, got %v", current["chronic_load"])
	}
}

// The offset is signed, and every week boundary in the view is cut with it. A
// western hemisphere coach is the case that would catch a shift applied the
// wrong way round, which a positive offset alone cannot.
func TestTrainingLoadCutsTheWeekOnANegativeOffset(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingLoadHandler: handler.NewCoachTrainingLoadHandler(queries, pool),
	})
	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "loadwestcoach-test@example.com")
	userID, _ := testutil.CreateTestUser(t, queries, "loadwestclient-test@example.com")
	enrollUserDirect(t, pool, coachID, userID)

	const offsetMinutes = -300
	shiftedNow := time.Now().UTC().Add(offsetMinutes * time.Minute)
	daysSinceMonday := (int(shiftedNow.Weekday()) + 6) % 7
	day := shiftedNow.AddDate(0, 0, -daysSinceMonday)
	monday := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)

	// Sunday 23:30 on the caller's clock, which is already Monday in UTC. It
	// belongs to the week that closes, not to the one that opens.
	lastMomentOfTheWeek := monday.Add(-30 * time.Minute)
	insertLoadSessionAt(t, pool, userID, lastMomentOfTheWeek, offsetMinutes, 1, 3600, rpeOf(7), false)

	url := fmt.Sprintf("/api/coach/clients/%s/training-load?weeks=2&tz_offset_minutes=%d", userID, offsetMinutes)
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

	if body.Weeks[1]["week_start"] != monday.Format(time.DateOnly) {
		t.Fatalf("Expected the current week to start on %s, got %v", monday.Format(time.DateOnly), body.Weeks[1]["week_start"])
	}
	if got := numberAt(t, body.Weeks[0], "session_count"); got != 1 {
		t.Fatalf("Expected the session in the week that closes, got %v", got)
	}
	if got := numberAt(t, body.Weeks[1], "session_count"); got != 0 {
		t.Fatalf("Expected nothing in the current week, got %v", got)
	}
}

// mondayInZone is the Monday of the week holding at, read in zone, at local
// midnight.
func mondayInZone(at time.Time, zone *time.Location) time.Time {
	local := at.In(zone)
	daysSinceMonday := (int(local.Weekday()) + 6) % 7
	day := local.AddDate(0, 0, -daysSinceMonday)
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, zone)
}

// weeksBackToTheLastSwitch counts the Mondays between the current one and the
// first one sitting on the other side of a daylight saving change. Derived from
// the zone rather than written down, so the test keeps pinning a real
// transition as the calendar moves past the one it was written against.
func weeksBackToTheLastSwitch(t *testing.T, zone *time.Location) int {
	t.Helper()
	thisMonday := mondayInZone(time.Now(), zone)
	_, nowOffset := thisMonday.Zone()
	for back := 1; back <= maxTrainingLoadTestWeeks; back++ {
		if _, offset := thisMonday.AddDate(0, 0, -7*back).Zone(); offset != nowOffset {
			return back
		}
	}
	t.Fatalf("Found no daylight saving change in the last year of %s", zone)
	return 0
}

// The endpoint caps the window it will read, so a test reaching back to a
// transition has to stay inside it. The widest gap between two changes is the
// summer one, about thirty weeks, and the window here is three weeks wider.
const maxTrainingLoadTestWeeks = 52

// A window reaching back across a daylight saving change cannot be cut with a
// single offset: whichever one the caller sends is right on one side of the
// change and an hour out on the other, which moves a session recorded near a
// Monday midnight into the neighbouring week. So the boundary is pinned from
// both sides of both offsets, which no single offset can satisfy, and the
// offset the caller would really send is passed alongside the zone to make sure
// the zone is the one that wins.
//
// Run east and west of UTC, since a local midnight falls on the previous UTC
// day in one and the same UTC day in the other.
func TestTrainingLoadCutsWeeksAcrossADaylightSavingChange(t *testing.T) {
	for _, zoneName := range []string{"Europe/Paris", "America/New_York"} {
		t.Run(zoneName, func(t *testing.T) {
			zone, err := time.LoadLocation(zoneName)
			if err != nil {
				t.Fatalf("Failed to load %s: %v", zoneName, err)
			}

			t.Setenv("JWT_SECRET", "test-secret-key")
			pool, queries := testutil.SetupTestDB(t)
			defer testutil.CleanupTestDB(t, pool)

			app := testutil.SetupFiberApp(testutil.HandlerConfig{
				CoachTrainingLoadHandler: handler.NewCoachTrainingLoadHandler(queries, pool),
			})
			coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "loaddstcoach-test@example.com")
			userID, _ := testutil.CreateTestUser(t, queries, "loaddstclient-test@example.com")
			enrollUserDirect(t, pool, coachID, userID)

			thisMonday := mondayInZone(time.Now(), zone)
			switchBack := weeksBackToTheLastSwitch(t, zone)

			// afterSwitch is the first Monday on this side of the change,
			// beforeSwitch one safely on the far side of it. The two boundaries
			// are two weeks apart, so the four sessions land in four weeks.
			afterSwitch := thisMonday.AddDate(0, 0, -7*(switchBack-1))
			beforeSwitch := thisMonday.AddDate(0, 0, -7*(switchBack+1))
			boundaries := []time.Time{afterSwitch, beforeSwitch}

			// Each boundary is pinned from both sides: the last half hour of
			// the week that closes and the first half hour of the week that
			// opens. Both zones switch at 02:00 or 03:00 local, so neither wall
			// clock time is one the change skips or repeats.
			for _, monday := range boundaries {
				insertLoadSessionInstant(t, pool, userID, monday.Add(-30*time.Minute), 1, 3600, rpeOf(7))
				insertLoadSessionInstant(t, pool, userID, monday.Add(30*time.Minute), 1, 3600, rpeOf(7))
			}

			weeksWanted := switchBack + 3
			if weeksWanted > maxTrainingLoadTestWeeks {
				t.Fatalf("The last change in %s is %d weeks back, too far for the window", zone, switchBack)
			}
			_, nowOffset := thisMonday.Zone()
			requestURL := fmt.Sprintf("/api/coach/clients/%s/training-load?weeks=%d&tz_offset_minutes=%d&timezone=%s",
				userID, weeksWanted, nowOffset/60, zoneName)
			resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, requestURL, nil, coachToken), fiber.TestConfig{Timeout: 10 * time.Second})
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

			byWeek := make(map[string]map[string]interface{}, len(body.Weeks))
			for _, week := range body.Weeks {
				byWeek[week["week_start"].(string)] = week
			}

			for _, monday := range boundaries {
				for _, want := range []struct{ weekStart, label string }{
					{monday.AddDate(0, 0, -7).Format(time.DateOnly), "the last half hour of the week that closes"},
					{monday.Format(time.DateOnly), "the first half hour of the week that opens"},
				} {
					week, found := byWeek[want.weekStart]
					if !found {
						t.Fatalf("Week %s is missing from the series", want.weekStart)
					}
					if got := numberAt(t, week, "session_count"); got != 1 {
						t.Fatalf("Expected %s of %s to land in week %s, found %v sessions there",
							want.label, monday.Format(time.DateOnly), want.weekStart, got)
					}
				}
			}

			// The athlete's history opens in the week the earliest of those
			// sessions fell in, which the handler works out in Go rather than
			// in the query. Placed a week either side it would read as a rest
			// week to average in, or as silence before the history started.
			firstWeek := beforeSwitch.AddDate(0, 0, -7).Format(time.DateOnly)
			if got := numberAt(t, byWeek[firstWeek], "chronic_weeks"); got != 1 {
				t.Fatalf("Expected week %s to be the first of the history and rest on itself alone, got %v weeks", firstWeek, got)
			}
		})
	}
}

// The zone is the caller's, not the server's, and not a name the database would
// choke on later. A malformed offset is still refused alongside a good zone,
// rather than being dropped unread because the zone would have won anyway.
func TestTrainingLoadRejectsAnUnusableTimezone(t *testing.T) {
	app, pool, _, _, userID, coachToken := setupTrainingLoadApp(t)
	defer testutil.CleanupTestDB(t, pool)

	for _, query := range []string{
		"timezone=" + url.QueryEscape("Europe/Nowhere"),
		"timezone=Local",
		"timezone=" + url.QueryEscape("../../etc/passwd"),
		"timezone=CEST",
		"timezone=" + url.QueryEscape("Europe/Paris") + "&tz_offset_minutes=9999",
	} {
		requestURL := fmt.Sprintf("/api/coach/clients/%s/training-load?%s", userID, query)
		resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, requestURL, nil, coachToken), fiber.TestConfig{Timeout: 10 * time.Second})
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Expected 400 for %s, got %d", query, resp.StatusCode)
		}
	}
}
