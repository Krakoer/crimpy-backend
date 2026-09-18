package handler_test

import (
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

type bodyweightFixture struct {
	app        *fiber.App
	coachToken string
	userID     string
	userToken  string
	pool       *pgxpool.Pool
	// Kept so a test that needs a second account makes one against this pool
	// rather than opening another and running the shared cleanup twice.
	queries *db.Queries
}

func setupBodyweightFixture(t *testing.T, emailPrefix string) bodyweightFixture {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, emailPrefix+"coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, emailPrefix+"user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		BodyweightHandler: handler.NewBodyweightHandler(queries, pool),
	})

	return bodyweightFixture{
		app:        app,
		coachToken: coachToken,
		userID:     userID,
		userToken:  userToken,
		pool:       pool,
		queries:    queries,
	}
}

func (f bodyweightFixture) record(t *testing.T, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req := testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/user/bodyweights", payload, f.userToken)
	resp, err := f.app.Test(req)
	if err != nil {
		t.Fatalf("Record request failed: %v", err)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func (f bodyweightFixture) list(t *testing.T, url, token string) (int, []interface{}) {
	t.Helper()
	resp, err := f.app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, url, nil, token))
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	var result []interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func TestBodyweight_RecordsAndReadsBackNewestFirst(t *testing.T) {
	f := setupBodyweightFixture(t, "bw1")

	for _, entry := range []struct {
		weight float64
		days   int
	}{{70.5, 14}, {71.2, 7}, {70.8, 0}} {
		measured := time.Now().UTC().AddDate(0, 0, -entry.days).Format(time.RFC3339)
		if status, body := f.record(t, map[string]interface{}{
			"weight_kg": entry.weight, "measured_at": measured,
		}); status != fiber.StatusCreated {
			t.Fatalf("Expected 201 recording %v, got %d: %v", entry.weight, status, body)
		}
	}

	status, series := f.list(t, "/api/user/bodyweights", f.userToken)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", status)
	}
	if len(series) != 3 {
		t.Fatalf("Expected 3 measurements, got %d", len(series))
	}
	// Newest first is what a trend is read from, and what the freeze takes.
	weights := make([]float64, 0, 3)
	for _, entry := range series {
		weights = append(weights, entry.(map[string]interface{})["weight_kg"].(float64))
	}
	expected := []float64{70.8, 71.2, 70.5}
	for i, want := range expected {
		if weights[i] != want {
			t.Errorf("Expected %v at position %d, got %v (series %v)", want, i, weights[i], weights)
		}
	}
}

// The measurement carries the day it was taken, not the day it arrived: a
// device that was offline sends it later and the series still reads in order.
func TestBodyweight_KeepsTheDayItWasMeasured(t *testing.T) {
	f := setupBodyweightFixture(t, "bw2")

	measured := time.Now().UTC().AddDate(0, 0, -30).Truncate(time.Second)
	status, body := f.record(t, map[string]interface{}{
		"weight_kg": 68.4, "measured_at": measured.Format(time.RFC3339),
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, body)
	}
	if got := body["measured_at"]; got != measured.Format(time.RFC3339) {
		t.Errorf("Expected measured_at %s, got %v", measured.Format(time.RFC3339), got)
	}
}

func TestBodyweight_DefaultsTheMeasurementToNow(t *testing.T) {
	f := setupBodyweightFixture(t, "bw3")

	before := time.Now().UTC().Add(-time.Minute)
	status, body := f.record(t, map[string]interface{}{"weight_kg": 72})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d: %v", status, body)
	}
	measured, err := time.Parse(time.RFC3339, body["measured_at"].(string))
	if err != nil {
		t.Fatalf("measured_at is not an instant: %v", body["measured_at"])
	}
	if measured.Before(before) {
		t.Errorf("Expected measured_at to default to now, got %v", measured)
	}
}

func TestBodyweight_RefusesAWeightThatIsNotOne(t *testing.T) {
	f := setupBodyweightFixture(t, "bw4")

	for _, weight := range []float64{0, -5, 501} {
		status, body := f.record(t, map[string]interface{}{"weight_kg": weight})
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for %v, got %d: %v", weight, status, body)
		}
	}
}

// A clock that is wrong would otherwise sit at the head of the series and be
// what every prescription freezes against until the real date catches up.
func TestBodyweight_RefusesAMeasurementFromTheFuture(t *testing.T) {
	f := setupBodyweightFixture(t, "bw5")

	future := time.Now().UTC().AddDate(0, 0, 2).Format(time.RFC3339)
	status, body := f.record(t, map[string]interface{}{"weight_kg": 70, "measured_at": future})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for a future measurement, got %d: %v", status, body)
	}
}

// The whole point of the series: a coach reads it, so a strength number can be
// read as a ratio rather than as an absolute.
func TestBodyweight_IsReadableByTheCoach(t *testing.T) {
	f := setupBodyweightFixture(t, "bw6")

	if status, _ := f.record(t, map[string]interface{}{"weight_kg": 71}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording the weight, got %d", status)
	}

	status, series := f.list(t, fmt.Sprintf("/api/coach/clients/%s/bodyweights", f.userID), f.coachToken)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 for the coach, got %d", status)
	}
	if len(series) != 1 {
		t.Fatalf("Expected 1 measurement, got %d", len(series))
	}
	if series[0].(map[string]interface{})["weight_kg"] != float64(71) {
		t.Errorf("Expected the coach to read 71, got %v", series[0])
	}
}

func TestBodyweight_IsNotReadableByAnotherCoach(t *testing.T) {
	f := setupBodyweightFixture(t, "bw7")
	_, strangerToken := testutil.CreateTestValidatedCoachUser(t, f.pool, f.queries, "bw7stranger@test.com")

	if status, _ := f.record(t, map[string]interface{}{"weight_kg": 71}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d", status)
	}

	status, _ := f.list(t, fmt.Sprintf("/api/coach/clients/%s/bodyweights", f.userID), strangerToken)
	if status != fiber.StatusForbidden {
		t.Errorf("Expected 403 for a coach the athlete is not enrolled with, got %d", status)
	}
}

func TestBodyweight_IsNotReadableByAnotherAthlete(t *testing.T) {
	f := setupBodyweightFixture(t, "bw8")
	_, strangerToken := testutil.CreateTestUser(t, f.queries, "bw8stranger@test.com")

	if status, _ := f.record(t, map[string]interface{}{"weight_kg": 71}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d", status)
	}

	// The athlete read is scoped to the caller, so a stranger reads their own
	// empty series rather than anyone else's.
	status, series := f.list(t, "/api/user/bodyweights", strangerToken)
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", status)
	}
	if len(series) != 0 {
		t.Errorf("Expected a stranger to read nothing, got %v", series)
	}
}

// Answered the way every other delete in this API answers: 403 for someone
// else's row and 404 for one that is not there, rather than telling a caller
// their delete succeeded when nothing was deleted.
func TestBodyweight_DeletesOnlyTheCallersOwn(t *testing.T) {
	f := setupBodyweightFixture(t, "bw9")
	_, strangerToken := testutil.CreateTestUser(t, f.queries, "bw9stranger@test.com")

	_, created := f.record(t, map[string]interface{}{"weight_kg": 71})
	id := created["id"].(string)

	resp, _ := f.app.Test(testutil.NewJSONRequestWithAuth(
		http.MethodDelete, "/api/user/bodyweights/"+id, nil, strangerToken))
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403 deleting someone else's measurement, got %d", resp.StatusCode)
	}
	if _, series := f.list(t, "/api/user/bodyweights", f.userToken); len(series) != 1 {
		t.Fatalf("Expected the measurement to survive a stranger's delete, got %v", series)
	}

	resp, _ = f.app.Test(testutil.NewJSONRequestWithAuth(
		http.MethodDelete, "/api/user/bodyweights/"+id, nil, f.userToken))
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200 deleting my own, got %d", resp.StatusCode)
	}
	if _, series := f.list(t, "/api/user/bodyweights", f.userToken); len(series) != 0 {
		t.Errorf("Expected the measurement to be gone, got %v", series)
	}

	resp, _ = f.app.Test(testutil.NewJSONRequestWithAuth(
		http.MethodDelete, "/api/user/bodyweights/"+id, nil, f.userToken))
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected 404 deleting it again, got %d", resp.StatusCode)
	}
}

func TestBodyweight_RefusesAnImpossibleLimit(t *testing.T) {
	f := setupBodyweightFixture(t, "bw10")

	for _, limit := range []string{"0", "-1", "366", "many"} {
		status, _ := f.list(t, "/api/user/bodyweights?limit="+limit, f.userToken)
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for limit=%s, got %d", limit, status)
		}
	}
}
