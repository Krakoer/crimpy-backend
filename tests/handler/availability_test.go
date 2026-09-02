package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// A Monday, so every week_start in these tests satisfies the schema check.
const testWeekStart = "2026-06-01"

func fullWeek(available map[int]map[string]interface{}) map[string]interface{} {
	days := make([]map[string]interface{}, 0, 7)
	for day := 0; day < 7; day++ {
		entry := map[string]interface{}{"day_of_week": day, "is_available": false}
		if override, ok := available[day]; ok {
			for k, v := range override {
				entry[k] = v
			}
		}
		days = append(days, entry)
	}
	return map[string]interface{}{"days": days}
}

func putWeek(t *testing.T, app *fiber.App, token, weekStart string, body map[string]interface{}) *http.Response {
	t.Helper()
	payload, _ := json.Marshal(body)
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/user/availability/"+weekStart, payload, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to put availability: %v", err)
	}
	return resp
}

func TestAvailability_UpsertAndRead(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail1user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	resp := putWeek(t, app, userToken, testWeekStart, fullWeek(map[int]map[string]interface{}{
		1: {"is_available": true, "duration_minutes": 90, "note": "gym after work"},
		3: {"is_available": true},
	}))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 declaring a week, got %d", resp.StatusCode)
	}

	var week map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&week)
	if week["week_start"] != testWeekStart {
		t.Errorf("Expected week_start %s, got %v", testWeekStart, week["week_start"])
	}
	days := week["days"].([]interface{})
	if len(days) != 7 {
		t.Fatalf("Expected 7 days, got %d", len(days))
	}
	tuesday := days[1].(map[string]interface{})
	if tuesday["is_available"] != true {
		t.Errorf("Expected tuesday available, got %v", tuesday["is_available"])
	}
	if tuesday["duration_minutes"].(float64) != 90 {
		t.Errorf("Expected 90 minutes, got %v", tuesday["duration_minutes"])
	}
	if tuesday["note"] != "gym after work" {
		t.Errorf("Expected the note back, got %v", tuesday["note"])
	}
	monday := days[0].(map[string]interface{})
	if _, present := monday["duration_minutes"]; present {
		t.Errorf("Expected no duration on an undeclared day, got %v", monday["duration_minutes"])
	}

	// A second athlete declaring the same week must not widen the first one's
	// list: this route carries no id in its URL, so a regression in the query
	// filter would be a silent cross-tenant leak.
	_, otherToken := testutil.CreateTestUser(t, queries, "avail1other@test.com")
	putWeek(t, app, otherToken, testWeekStart, fullWeek(map[int]map[string]interface{}{
		5: {"is_available": true},
	}))

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/user/availability", nil, userToken)
	listResp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to list availability: %v", err)
	}
	var weeks []map[string]interface{}
	json.NewDecoder(listResp.Body).Decode(&weeks)
	if len(weeks) != 1 {
		t.Fatalf("Expected 1 declared week, got %d", len(weeks))
	}
	if len(weeks[0]["days"].([]interface{})) != 7 {
		t.Errorf("Expected the listed week to carry 7 days")
	}
	saturday := weeks[0]["days"].([]interface{})[5].(map[string]interface{})
	if saturday["is_available"] != false {
		t.Errorf("Expected the other athlete's saturday not to leak into this list")
	}
}

func TestAvailability_UnavailableDayDropsItsDuration(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail11user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	// The app leaves a duration behind when a filled day is toggled off, and a
	// row saying "not available for 120 minutes" has no reading for a coach.
	resp := putWeek(t, app, userToken, testWeekStart, fullWeek(map[int]map[string]interface{}{
		2: {"is_available": false, "duration_minutes": 120, "note": "travelling"},
	}))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	var week map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&week)
	wednesday := week["days"].([]interface{})[2].(map[string]interface{})
	if _, present := wednesday["duration_minutes"]; present {
		t.Errorf("Expected the duration dropped on an unavailable day, got %v", wednesday["duration_minutes"])
	}
	// The note survives: it is the whole point of a permissive form.
	if wednesday["note"] != "travelling" {
		t.Errorf("Expected the note kept on an unavailable day, got %v", wednesday["note"])
	}
}

func TestAvailability_SecondPutReplaces(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail2user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	putWeek(t, app, userToken, testWeekStart, fullWeek(map[int]map[string]interface{}{
		1: {"is_available": true, "duration_minutes": 90},
	}))
	resp := putWeek(t, app, userToken, testWeekStart, fullWeek(map[int]map[string]interface{}{
		2: {"is_available": true},
	}))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 on the second write, got %d", resp.StatusCode)
	}

	var week map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&week)
	days := week["days"].([]interface{})
	if len(days) != 7 {
		t.Fatalf("Expected the week to stay at 7 days, got %d", len(days))
	}
	tuesday := days[1].(map[string]interface{})
	if tuesday["is_available"] != false {
		t.Errorf("Expected tuesday cleared by the second write, got %v", tuesday["is_available"])
	}
	if _, present := tuesday["duration_minutes"]; present {
		t.Errorf("Expected the old duration cleared, got %v", tuesday["duration_minutes"])
	}
	wednesday := days[2].(map[string]interface{})
	if wednesday["is_available"] != true {
		t.Errorf("Expected wednesday available after the second write")
	}
}

func TestAvailability_InvalidInput(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail3user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	sixDays := fullWeek(nil)
	sixDays["days"] = sixDays["days"].([]map[string]interface{})[:6]

	duplicate := fullWeek(nil)
	duplicate["days"].([]map[string]interface{})[6]["day_of_week"] = 0

	outOfRange := fullWeek(nil)
	outOfRange["days"].([]map[string]interface{})[6]["day_of_week"] = 7

	cases := []struct {
		name      string
		weekStart string
		body      map[string]interface{}
	}{
		{"not a monday", "2026-06-02", fullWeek(nil)},
		{"unparseable date", "not-a-date", fullWeek(nil)},
		{"six days", testWeekStart, sixDays},
		{"duplicate day", testWeekStart, duplicate},
		{"day out of range", testWeekStart, outOfRange},
		{"zero duration", testWeekStart, fullWeek(map[int]map[string]interface{}{
			1: {"is_available": true, "duration_minutes": 0},
		})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := putWeek(t, app, userToken, tc.weekStart, tc.body)
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Errorf("Expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestAvailability_CoachReadsEnrolledClient(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "avail4coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "avail4user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	putWeek(t, app, userToken, testWeekStart, fullWeek(map[int]map[string]interface{}{
		4: {"is_available": true, "note": "long session"},
	}))

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/clients/"+userID+"/availability", nil, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to read client availability: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	var weeks []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&weeks)
	if len(weeks) != 1 {
		t.Fatalf("Expected 1 declared week, got %d", len(weeks))
	}
	friday := weeks[0]["days"].([]interface{})[4].(map[string]interface{})
	if friday["note"] != "long session" {
		t.Errorf("Expected the coach to read the note, got %v", friday["note"])
	}
}

func TestAvailability_CoachCannotReadUnenrolledClient(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "avail5coach@test.com")
	otherUserID, otherToken := testutil.CreateTestUser(t, queries, "avail5other@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})
	putWeek(t, app, otherToken, testWeekStart, fullWeek(nil))

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/clients/"+otherUserID+"/availability", nil, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to issue the request: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403 reading a client not enrolled with this coach, got %d", resp.StatusCode)
	}
}

func TestAvailability_UnvalidatedCoachRefused(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, pendingToken := testutil.CreateTestCoachUser(t, queries, "avail6coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "avail6user@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/clients/"+userID+"/availability", nil, pendingToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to issue the request: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403 for a coach awaiting validation, got %d", resp.StatusCode)
	}
}

func TestAvailabilityReminder_SetAndRead(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "avail7coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "avail7user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"enabled": true, "day_of_week": 4, "hour": 21, "minute": 0,
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/coach/availability-reminder", body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to set the reminder: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 setting the reminder, got %d", resp.StatusCode)
	}

	req = testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/availability-reminder", nil, coachToken)
	resp, _ = app.Test(req)
	var reminder map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&reminder)
	if reminder["hour"].(float64) != 21 || reminder["day_of_week"].(float64) != 4 {
		t.Errorf("Expected Friday 21h back, got %v", reminder)
	}

	// The athlete reads the reminder their own coach configured.
	req = testutil.NewRequestWithAuth(http.MethodGet, "/api/user/availability-reminder", nil, userToken)
	resp, _ = app.Test(req)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 for the enrolled athlete, got %d", resp.StatusCode)
	}
	json.NewDecoder(resp.Body).Decode(&reminder)
	if reminder["enabled"] != true {
		t.Errorf("Expected the reminder enabled, got %v", reminder["enabled"])
	}
}

func TestAvailabilityReminder_NoCoachIsNotFound(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail8user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/user/availability-reminder", nil, userToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to issue the request: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected 404 for an athlete with no coach, got %d", resp.StatusCode)
	}
}

func TestAvailabilityReminder_NonCoachRefused(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail9user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"enabled": true, "day_of_week": 4, "hour": 21, "minute": 0,
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/coach/availability-reminder", body, userToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to issue the request: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403 for a non coach, got %d", resp.StatusCode)
	}

	req = testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/availability-reminder", nil, userToken)
	resp, _ = app.Test(req)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403 reading the coach reminder as a non coach, got %d", resp.StatusCode)
	}
}

func TestAvailabilityReminder_InvalidInput(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "avail10coach@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	cases := []struct {
		name string
		body map[string]interface{}
	}{
		{"day out of range", map[string]interface{}{"enabled": true, "day_of_week": 7, "hour": 21, "minute": 0}},
		{"hour out of range", map[string]interface{}{"enabled": true, "day_of_week": 4, "hour": 24, "minute": 0}},
		{"minute out of range", map[string]interface{}{"enabled": true, "day_of_week": 4, "hour": 21, "minute": 60}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			req := testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/coach/availability-reminder", body, coachToken)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Failed to issue the request: %v", err)
			}
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Errorf("Expected 400, got %d", resp.StatusCode)
			}
		})
	}
}
