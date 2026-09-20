package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// A Monday, so every week_start in these tests satisfies the schema check.
const testWeekStart = "2026-06-01"

// activity builds one entry of a day's list. Only the label is required, so the
// optional fields are left out when nil rather than sent as null.
func activity(label string, fields map[string]interface{}) map[string]interface{} {
	entry := map[string]interface{}{"label": label}
	for k, v := range fields {
		entry[k] = v
	}
	return entry
}

// fullWeek builds the seven days the API demands, with the days named in
// "planned" carrying activities and the rest carrying none.
func fullWeek(planned map[int][]map[string]interface{}) map[string]interface{} {
	days := make([]map[string]interface{}, 0, 7)
	for day := 0; day < 7; day++ {
		activities := planned[day]
		if activities == nil {
			activities = []map[string]interface{}{}
		}
		days = append(days, map[string]interface{}{
			"day_of_week": day,
			"activities":  activities,
		})
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

func dayActivities(t *testing.T, week map[string]interface{}, dayOfWeek int) []interface{} {
	t.Helper()
	days, ok := week["days"].([]interface{})
	if !ok {
		t.Fatalf("Expected the week to carry days, got %v", week["days"])
	}
	if len(days) != 7 {
		t.Fatalf("Expected 7 days, got %d", len(days))
	}
	day := days[dayOfWeek].(map[string]interface{})
	list, ok := day["activities"].([]interface{})
	if !ok {
		t.Fatalf("Expected day %d to carry an activities list, got %v", dayOfWeek, day["activities"])
	}
	return list
}

func listWeeks(t *testing.T, app *fiber.App, token string) []map[string]interface{} {
	t.Helper()
	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/user/availability", nil, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to list availability: %v", err)
	}
	var weeks []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&weeks)
	return weeks
}

func TestAvailability_UpsertAndRead(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail1user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	resp := putWeek(t, app, userToken, testWeekStart, fullWeek(map[int][]map[string]interface{}{
		1: {
			activity("Bouldering", map[string]interface{}{
				"duration_minutes": 90, "when": "after work", "where": "Arkose",
			}),
			activity("Stretching", map[string]interface{}{"duration_minutes": 20}),
		},
		3: {activity("Long run", nil)},
	}))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 declaring a week, got %d", resp.StatusCode)
	}

	var week map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&week)
	if week["week_start"] != testWeekStart {
		t.Errorf("Expected week_start %s, got %v", testWeekStart, week["week_start"])
	}

	// Two activities on one day is the whole point of the shape: the old schema
	// could not hold them.
	tuesday := dayActivities(t, week, 1)
	if len(tuesday) != 2 {
		t.Fatalf("Expected 2 activities on tuesday, got %d", len(tuesday))
	}
	first := tuesday[0].(map[string]interface{})
	if first["label"] != "Bouldering" {
		t.Errorf("Expected the first activity back, got %v", first["label"])
	}
	if first["duration_minutes"].(float64) != 90 {
		t.Errorf("Expected 90 minutes, got %v", first["duration_minutes"])
	}
	if first["when"] != "after work" || first["where"] != "Arkose" {
		t.Errorf("Expected when and where back, got %v and %v", first["when"], first["where"])
	}
	// The order the athlete entered them in is the order a coach reads them.
	if tuesday[1].(map[string]interface{})["label"] != "Stretching" {
		t.Errorf("Expected the second activity to stay second, got %v", tuesday[1])
	}

	thursday := dayActivities(t, week, 3)
	if len(thursday) != 1 {
		t.Fatalf("Expected 1 activity on thursday, got %d", len(thursday))
	}
	if _, present := thursday[0].(map[string]interface{})["duration_minutes"]; present {
		t.Errorf("Expected no duration on an activity that gave none")
	}

	if len(dayActivities(t, week, 0)) != 0 {
		t.Errorf("Expected monday to carry no activity")
	}

	// A second athlete declaring the same week must not widen the first one's
	// list: this route carries no id in its URL, so a regression in the query
	// filter would be a silent cross-tenant leak.
	_, otherToken := testutil.CreateTestUser(t, queries, "avail1other@test.com")
	putWeek(t, app, otherToken, testWeekStart, fullWeek(map[int][]map[string]interface{}{
		5: {activity("Somebody else's climb", nil)},
	}))

	weeks := listWeeks(t, app, userToken)
	if len(weeks) != 1 {
		t.Fatalf("Expected 1 declared week, got %d", len(weeks))
	}
	if len(dayActivities(t, weeks[0], 5)) != 0 {
		t.Errorf("Expected the other athlete's saturday not to leak into this list")
	}
}

// The invariant the whole reshape turns on. Under an unbounded list a week
// where nothing is planned holds no activity row at all, and a declaration read
// off the rows would read that week as never answered, so the reminder would
// keep nudging an athlete who has already said they have nothing on.
func TestAvailability_EmptyWeekStillReadsAsDeclared(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail7user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	resp := putWeek(t, app, userToken, testWeekStart, fullWeek(nil))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 declaring an empty week, got %d", resp.StatusCode)
	}

	weeks := listWeeks(t, app, userToken)
	if len(weeks) != 1 {
		t.Fatalf("Expected the empty week to read back as declared, got %d weeks", len(weeks))
	}
	if weeks[0]["week_start"] != testWeekStart {
		t.Errorf("Expected week_start %s, got %v", testWeekStart, weeks[0]["week_start"])
	}
	for day := 0; day < 7; day++ {
		if len(dayActivities(t, weeks[0], day)) != 0 {
			t.Errorf("Expected day %d to carry no activity", day)
		}
	}
}

// Emptying a week the athlete had filled must not take the week off the list
// either: it is a correction, not a retraction of the answer.
func TestAvailability_ClearingAWeekKeepsItDeclared(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail8user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	putWeek(t, app, userToken, testWeekStart, fullWeek(map[int][]map[string]interface{}{
		1: {activity("Bouldering", nil)},
	}))
	resp := putWeek(t, app, userToken, testWeekStart, fullWeek(nil))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 clearing the week, got %d", resp.StatusCode)
	}

	weeks := listWeeks(t, app, userToken)
	if len(weeks) != 1 {
		t.Fatalf("Expected the cleared week to stay declared, got %d weeks", len(weeks))
	}
	if len(dayActivities(t, weeks[0], 1)) != 0 {
		t.Errorf("Expected tuesday cleared by the second write")
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

	putWeek(t, app, userToken, testWeekStart, fullWeek(map[int][]map[string]interface{}{
		1: {
			activity("Bouldering", map[string]interface{}{"duration_minutes": 90}),
			activity("Stretching", nil),
		},
	}))
	resp := putWeek(t, app, userToken, testWeekStart, fullWeek(map[int][]map[string]interface{}{
		2: {activity("Fingerboard", nil)},
	}))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 on the second write, got %d", resp.StatusCode)
	}

	var week map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&week)
	if len(dayActivities(t, week, 1)) != 0 {
		t.Errorf("Expected tuesday cleared by the second write")
	}
	wednesday := dayActivities(t, week, 2)
	if len(wednesday) != 1 || wednesday[0].(map[string]interface{})["label"] != "Fingerboard" {
		t.Errorf("Expected wednesday to hold the new activity, got %v", wednesday)
	}

	// The week is one answer, so re-declaring it must not add a second one.
	if weeks := listWeeks(t, app, userToken); len(weeks) != 1 {
		t.Errorf("Expected the re-declared week to stay a single week, got %d", len(weeks))
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

	tooMany := make([]map[string]interface{}, 0, 21)
	for i := 0; i < 21; i++ {
		tooMany = append(tooMany, activity("Climb", nil))
	}

	// The body an app built against the previous shape sends: seven days, each
	// describing itself with is_available and a note and carrying no activities
	// key at all. Read as an empty list it would answer 200 and erase the week
	// the athlete was trying to write, so it has to be refused.
	previousShape := map[string]interface{}{"days": func() []map[string]interface{} {
		days := make([]map[string]interface{}, 0, 7)
		for day := 0; day < 7; day++ {
			days = append(days, map[string]interface{}{
				"day_of_week":  day,
				"is_available": day == 1,
				"note":         "gym after work",
			})
		}
		return days
	}()}

	nullActivities := fullWeek(nil)
	nullActivities["days"].([]map[string]interface{})[3]["activities"] = nil

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
		{"zero duration", testWeekStart, fullWeek(map[int][]map[string]interface{}{
			1: {activity("Bouldering", map[string]interface{}{"duration_minutes": 0})},
		})},
		{"blank label", testWeekStart, fullWeek(map[int][]map[string]interface{}{
			1: {activity("   ", nil)},
		})},
		{"label too long", testWeekStart, fullWeek(map[int][]map[string]interface{}{
			1: {activity(strings.Repeat("a", 201), nil)},
		})},
		{"when too long", testWeekStart, fullWeek(map[int][]map[string]interface{}{
			1: {activity("Bouldering", map[string]interface{}{"when": strings.Repeat("a", 201)})},
		})},
		{"where too long", testWeekStart, fullWeek(map[int][]map[string]interface{}{
			1: {activity("Bouldering", map[string]interface{}{"where": strings.Repeat("a", 201)})},
		})},
		{"too many activities on a day", testWeekStart, fullWeek(map[int][]map[string]interface{}{
			1: tooMany,
		})},
		{"a day carrying no activities key", testWeekStart, previousShape},
		{"a day whose activities are null", testWeekStart, nullActivities},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := putWeek(t, app, userToken, tc.weekStart, tc.body)
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Errorf("Expected 400, got %d", resp.StatusCode)
			}
		})
	}

	// A rejected write must not have left a declaration behind: the athlete
	// never answered, and the reminder has to keep saying so.
	if weeks := listWeeks(t, app, userToken); len(weeks) != 0 {
		t.Errorf("Expected no week declared by the refused writes, got %d", len(weeks))
	}
}

// An app still on the previous request shape must not be able to erase a week
// it thinks it is writing. The fields it sends are ignored by the new binding,
// so the day would bind with no activities, and a write that read that as
// "nothing planned" would answer 200 over the top of a full week.
func TestAvailability_PreviousRequestShapeLeavesTheWeekAlone(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail9user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	putWeek(t, app, userToken, testWeekStart, fullWeek(map[int][]map[string]interface{}{
		1: {activity("Bouldering", map[string]interface{}{"duration_minutes": 90})},
		3: {activity("Lead climbing", nil)},
	}))

	oldShape := map[string]interface{}{"days": func() []map[string]interface{} {
		days := make([]map[string]interface{}, 0, 7)
		for day := 0; day < 7; day++ {
			days = append(days, map[string]interface{}{
				"day_of_week":  day,
				"is_available": day == 1,
			})
		}
		return days
	}()}

	resp := putWeek(t, app, userToken, testWeekStart, oldShape)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for the previous request shape, got %d", resp.StatusCode)
	}

	weeks := listWeeks(t, app, userToken)
	if len(weeks) != 1 {
		t.Fatalf("Expected the week to survive, got %d weeks", len(weeks))
	}
	if len(dayActivities(t, weeks[0], 1)) != 1 || len(dayActivities(t, weeks[0], 3)) != 1 {
		t.Errorf("Expected the refused write to leave both days as they were")
	}
}

// The length rule is about what gets stored, so a value that only passes the
// limit because of surrounding whitespace is accepted and stored trimmed.
func TestAvailability_LengthIsMeasuredOnWhatIsStored(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail10user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	label := strings.Repeat("a", 200)
	resp := putWeek(t, app, userToken, testWeekStart, fullWeek(map[int][]map[string]interface{}{
		1: {activity("  "+label+"  ", nil)},
	}))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 for a label that is at the limit once trimmed, got %d", resp.StatusCode)
	}

	var week map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&week)
	tuesday := dayActivities(t, week, 1)
	if len(tuesday) != 1 || tuesday[0].(map[string]interface{})["label"] != label {
		t.Errorf("Expected the label stored trimmed, got %v", tuesday)
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

	putWeek(t, app, userToken, testWeekStart, fullWeek(map[int][]map[string]interface{}{
		4: {activity("Long session", map[string]interface{}{"where": "home board"})},
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
	friday := dayActivities(t, weeks[0], 4)
	if len(friday) != 1 {
		t.Fatalf("Expected 1 activity on friday, got %d", len(friday))
	}
	if friday[0].(map[string]interface{})["label"] != "Long session" {
		t.Errorf("Expected the coach to read the activity, got %v", friday[0])
	}
	if friday[0].(map[string]interface{})["where"] != "home board" {
		t.Errorf("Expected the coach to read where, got %v", friday[0])
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

// The weeks the window tests declare: six consecutive Mondays, written out as
// literals rather than derived from the bounds under test. A boundary computed
// from the same expression as the code agrees with whatever that expression
// becomes, and would survive the window being moved or switched off.
//
// The windows below run 2026-05-18 to 2026-06-08, so 2026-05-11 sits one week
// outside the near end and 2026-06-15 one week outside the far end.
func allWindowWeeks() []string {
	return []string{"2026-05-11", "2026-05-18", "2026-05-25", "2026-06-01", "2026-06-08", "2026-06-15"}
}

func listWeeksWithQuery(t *testing.T, app *fiber.App, token, query string) (*http.Response, []map[string]interface{}) {
	t.Helper()
	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/user/availability"+query, nil, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to list availability: %v", err)
	}
	var weeks []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&weeks)
	return resp, weeks
}

func weekStarts(weeks []map[string]interface{}) []string {
	starts := make([]string, 0, len(weeks))
	for _, week := range weeks {
		starts = append(starts, week["week_start"].(string))
	}
	return starts
}

func declaredWeeks(t *testing.T, app *fiber.App, token, query string) (*http.Response, []string) {
	t.Helper()
	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/user/availability/declared-weeks"+query, nil, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to list declared weeks: %v", err)
	}
	var raw []string
	json.NewDecoder(resp.Body).Decode(&raw)
	return resp, raw
}

func declareWindowWeeks(t *testing.T, app *fiber.App, token string) {
	t.Helper()
	for _, week := range allWindowWeeks() {
		resp := putWeek(t, app, token, week, fullWeek(map[int][]map[string]interface{}{
			2: {activity("Session in "+week, nil)},
		}))
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("Expected 200 declaring %s, got %d", week, resp.StatusCode)
		}
	}
}

// The window keeps the weeks on its bounds and drops the ones a single week
// either side of them, which is the pair of mistakes a range gets wrong.
func TestAvailability_WindowKeepsItsBoundsAndDropsTheWeeksOutside(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail11user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})
	declareWindowWeeks(t, app, userToken)

	resp, weeks := listWeeksWithQuery(t, app, userToken, "?from=2026-05-18&to=2026-06-08")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	got := weekStarts(weeks)
	want := []string{"2026-05-18", "2026-05-25", "2026-06-01", "2026-06-08"}
	if len(got) != len(want) {
		t.Fatalf("Expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Expected %v, got %v", want, got)
		}
	}

	// A week on the bound keeps the activities planned in it: the declarations
	// and the activities are filtered by the same window, so neither can come
	// back without the other.
	wednesday := dayActivities(t, weeks[0], 2)
	if len(wednesday) != 1 || wednesday[0].(map[string]interface{})["label"] != "Session in 2026-05-18" {
		t.Errorf("Expected the first week in the window to carry its activity, got %v", wednesday)
	}
	wednesday = dayActivities(t, weeks[3], 2)
	if len(wednesday) != 1 || wednesday[0].(map[string]interface{})["label"] != "Session in 2026-06-08" {
		t.Errorf("Expected the last week in the window to carry its activity, got %v", wednesday)
	}
}

// An absent parameter is an unbounded end, on either side and on both.
func TestAvailability_AbsentWindowIsUnbounded(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail12user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})
	declareWindowWeeks(t, app, userToken)

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{
			"no window at all reads every declared week",
			"",
			[]string{"2026-05-11", "2026-05-18", "2026-05-25", "2026-06-01", "2026-06-08", "2026-06-15"},
		},
		{
			"from alone keeps its own week and everything after it",
			"?from=2026-05-18",
			[]string{"2026-05-18", "2026-05-25", "2026-06-01", "2026-06-08", "2026-06-15"},
		},
		{
			"to alone keeps its own week and everything before it",
			"?to=2026-06-08",
			[]string{"2026-05-11", "2026-05-18", "2026-05-25", "2026-06-01", "2026-06-08"},
		},
		{
			"a window of one week keeps that week alone",
			"?from=2026-05-25&to=2026-05-25",
			[]string{"2026-05-25"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, weeks := listWeeksWithQuery(t, app, userToken, tc.query)
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("Expected 200, got %d", resp.StatusCode)
			}
			got := weekStarts(weeks)
			if len(got) != len(tc.want) {
				t.Fatalf("Expected %v, got %v", tc.want, got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("Expected %v, got %v", tc.want, got)
				}
			}
		})
	}
}

func TestAvailability_InvalidWindowRefused(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail13user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	cases := []struct {
		name  string
		query string
	}{
		{"from is not a date", "?from=last-monday"},
		{"to is not a date", "?to=2026-13-40"},
		{"from is not a Monday", "?from=2026-05-19"},
		{"to is not a Monday", "?to=2026-06-07"},
		{"to is before from", "?from=2026-06-08&to=2026-05-18"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, _ := listWeeksWithQuery(t, app, userToken, tc.query)
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Errorf("Expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

// The declared weeks endpoint is what the app plans its reminders from, so it
// answers every week whatever the caller asks for. A window handed to it is not
// an error and is not applied: truncating this set is what brings a nudge back
// for a week the athlete already answered.
func TestAvailability_DeclaredWeeksIgnoreTheWindow(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail14user@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "avail14other@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})
	declareWindowWeeks(t, app, userToken)
	putWeek(t, app, otherToken, "2026-07-06", fullWeek(nil))

	want := []string{"2026-05-11", "2026-05-18", "2026-05-25", "2026-06-01", "2026-06-08", "2026-06-15"}
	for _, query := range []string{"", "?from=2026-05-25&to=2026-05-25"} {
		resp, got := declaredWeeks(t, app, userToken, query)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		if len(got) != len(want) {
			t.Fatalf("Expected every declared week %v for query %q, got %v", want, query, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("Expected %v for query %q, got %v", want, query, got)
			}
		}
	}

	// Another athlete's weeks are not in the answer: the endpoint is keyed to
	// the caller, and a leak here would nudge from someone else's schedule.
	_, othersWeeks := declaredWeeks(t, app, otherToken, "")
	if len(othersWeeks) != 1 || othersWeeks[0] != "2026-07-06" {
		t.Errorf("Expected the other athlete to read only their own week, got %v", othersWeeks)
	}
}

func TestAvailability_DeclaredWeeksEmptyWhenNothingDeclared(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "avail15user@test.com")
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/user/availability/declared-weeks", nil, userToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to list declared weeks: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("Expected an empty list rather than null, got %s", body)
	}
}

// The coach endpoint takes the same window, and the enrollment check still runs
// before it.
func TestAvailability_CoachWindowsClientWeeks(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "avail16coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "avail16user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AvailabilityHandler: handler.NewAvailabilityHandler(queries, pool),
	})
	declareWindowWeeks(t, app, userToken)

	readClient := func(query string) (*http.Response, []map[string]interface{}) {
		req := testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/clients/"+userID+"/availability"+query, nil, coachToken)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to read client availability: %v", err)
		}
		var weeks []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&weeks)
		return resp, weeks
	}

	resp, weeks := readClient("?from=2026-05-18&to=2026-06-08")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	got := weekStarts(weeks)
	want := []string{"2026-05-18", "2026-05-25", "2026-06-01", "2026-06-08"}
	if len(got) != len(want) {
		t.Fatalf("Expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Expected %v, got %v", want, got)
		}
	}

	resp, weeks = readClient("")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 without a window, got %d", resp.StatusCode)
	}
	if len(weeks) != len(allWindowWeeks()) {
		t.Errorf("Expected every declared week without a window, got %v", weekStarts(weeks))
	}

	resp, _ = readClient("?from=2026-05-19")
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for a bound that is not a Monday, got %d", resp.StatusCode)
	}
}
