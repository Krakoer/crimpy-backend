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
	"github.com/google/uuid"
)

// The one answer every override item id the session's training does not hold
// gets back, whoever owns it and whether or not it exists.
const unknownOverrideItemError = "unknown item_id in session 0 override"

// The one answer every training id the coach does not own gets back, whether or
// not it exists.
const unknownTrainingError = "unknown training_id at session 0"

func createTestCoachTrainingWithItems(t *testing.T, coachToken string, app *fiber.App) (trainingID, itemID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Hangboard Training",
		"items": []map[string]interface{}{
			{
				"type":                "repeater",
				"reps":                6,
				"hb_worktime_seconds": 7,
				"rest_seconds":        60,
				"hand":                "both",
				"granularity":         "uniform",
				"loads":               []map[string]interface{}{{"value": 0, "unit": "bw"}},
			},
		},
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/trainings", body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create coach training: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating coach training, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	trainingID = result["id"].(string)
	items := result["items"].([]interface{})
	item := items[0].(map[string]interface{})
	itemID = item["id"].(string)
	return trainingID, itemID
}

func weekURL(userID, programID string, weekNum int) string {
	return fmt.Sprintf("/api/coach/clients/%s/programs/%s/weeks/%d", userID, programID, weekNum)
}

func weeksURL(userID, programID string) string {
	return fmt.Sprintf("/api/coach/clients/%s/programs/%s/weeks", userID, programID)
}

func TestWeekHandler_UpsertWeek_Create(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk1coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk1user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"notes": "First heavy week",
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0, "notes": "Focus on max load"},
			{"training_id": trainingID, "times_per_week": 2},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if result["week_number"] != float64(1) {
		t.Errorf("Expected week_number 1, got %v", result["week_number"])
	}
	if result["notes"] != "First heavy week" {
		t.Errorf("Expected notes, got %v", result["notes"])
	}
	sessions, ok := result["sessions"].([]interface{})
	if !ok || len(sessions) != 2 {
		t.Errorf("Expected 2 sessions, got %v", result["sessions"])
	}
	s0 := sessions[0].(map[string]interface{})
	if s0["day_of_week"] != float64(0) {
		t.Errorf("Expected day_of_week 0 on first session, got %v", s0["day_of_week"])
	}
}

func TestWeekHandler_UpsertWeek_Update(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk2coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk2user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	first, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
			{"training_id": trainingID, "day_of_week": 2},
			{"training_id": trainingID, "day_of_week": 4},
		},
	})
	app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), first, coachToken))

	updated, _ := json.Marshal(map[string]interface{}{
		"notes":    "Updated notes",
		"sessions": []map[string]interface{}{{"training_id": trainingID, "times_per_week": 1}},
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), updated, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["notes"] != "Updated notes" {
		t.Errorf("Expected updated notes, got %v", result["notes"])
	}
	sessions := result["sessions"].([]interface{})
	if len(sessions) != 1 {
		t.Errorf("Expected sessions replaced (1), got %d", len(sessions))
	}
}

func TestWeekHandler_UpsertWeek_WithOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk3coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk3user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 1,
				"overrides": []map[string]interface{}{
					{
						"item_id": itemID,
						"overrides": map[string]interface{}{
							"loads": []map[string]interface{}{{"value": 35, "unit": "kg"}},
							"reps":  6,
						},
					},
				},
			},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	sessions := result["sessions"].([]interface{})
	session := sessions[0].(map[string]interface{})
	overrides, ok := session["overrides"].([]interface{})
	if !ok || len(overrides) != 1 {
		t.Errorf("Expected 1 override, got %v", session["overrides"])
	}
	o := overrides[0].(map[string]interface{})
	if o["item_id"] != itemID {
		t.Errorf("Expected item_id %s, got %v", itemID, o["item_id"])
	}
	if o["overrides"] == nil {
		t.Errorf("Expected overrides JSONB in response")
	}
}

func TestWeekHandler_UpsertWeek_InvalidSession_BothFields(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk4coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk4user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0, "times_per_week": 2},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400, got %d", resp.StatusCode)
	}
}

func TestWeekHandler_UpsertWeek_InvalidSession_NoField(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk5coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk5user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400, got %d", resp.StatusCode)
	}
}

func TestWeekHandler_UpsertWeek_InvalidWeekNumber(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk6coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk6user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)

	body, _ := json.Marshal(map[string]interface{}{"sessions": []interface{}{}})
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 0), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for week_number=0, got %d", resp.StatusCode)
	}
}

func TestWeekHandler_UpsertWeek_NotOwner(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk7coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk7user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	coach2ID, coach2Token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk7coach2@test.com")
	user2ID, _ := testutil.CreateTestUser(t, queries, "wk7user2@test.com")
	enrollUserDirect(t, pool, coach2ID, user2ID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)

	body, _ := json.Marshal(map[string]interface{}{"sessions": []interface{}{}})
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, fmt.Sprintf("/api/coach/clients/%s/programs/%s/weeks/1", user2ID, programID), body, coach2Token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403, got %d", resp.StatusCode)
	}
}

func TestWeekHandler_GetWeeks(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk8coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk8user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	for _, wn := range []int{1, 3} {
		body, _ := json.Marshal(map[string]interface{}{
			"sessions": []map[string]interface{}{
				{"training_id": trainingID, "day_of_week": 0},
			},
		})
		app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, wn), body, coachToken))
	}

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, weeksURL(userID, programID), nil, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result []interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 2 {
		t.Errorf("Expected 2 weeks, got %d", len(result))
	}
}

func TestWeekHandler_GetWeek(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk9coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk9user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 8}},
				},
			},
		},
	})
	app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 2), body, coachToken))

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, weekURL(userID, programID, 2), nil, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	sessions := result["sessions"].([]interface{})
	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}
	s := sessions[0].(map[string]interface{})
	if s["training_title"] == nil || s["training_title"] == "" {
		t.Errorf("Expected training_title populated, got %v", s["training_title"])
	}
	overrides := s["overrides"].([]interface{})
	if len(overrides) != 1 {
		t.Errorf("Expected 1 override, got %d", len(overrides))
	}
}

func TestWeekHandler_GetWeek_NotFound(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk10coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk10user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, weekURL(userID, programID, 99), nil, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected 404, got %d", resp.StatusCode)
	}
}

func TestWeekHandler_DeleteWeek(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk11coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk11user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
		},
	})
	app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken))

	delReq := testutil.NewJSONRequestWithAuth(http.MethodDelete, weekURL(userID, programID, 1), nil, coachToken)
	delResp, err := app.Test(delReq)
	if err != nil {
		t.Fatalf("Delete request failed: %v", err)
	}
	if delResp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200 on delete, got %d", delResp.StatusCode)
	}

	getReq := testutil.NewJSONRequestWithAuth(http.MethodGet, weekURL(userID, programID, 1), nil, coachToken)
	getResp, _ := app.Test(getReq)
	if getResp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected 404 after delete, got %d", getResp.StatusCode)
	}
}

func TestWeekHandler_GetMyWeeks(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk12coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "wk12user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	for _, wn := range []int{1, 2} {
		body, _ := json.Marshal(map[string]interface{}{
			"sessions": []map[string]interface{}{
				{"training_id": trainingID, "day_of_week": 0},
			},
		})
		app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, wn), body, coachToken))
	}

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/user/programs/%s/weeks", programID), nil, userToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result []interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 2 {
		t.Errorf("Expected 2 weeks, got %d", len(result))
	}
}

func TestWeekHandler_GetMyWeek(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk13coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "wk13user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
				},
			},
		},
	})
	app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken))

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/user/programs/%s/weeks/1", programID), nil, userToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	sessions := result["sessions"].([]interface{})
	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}
	s := sessions[0].(map[string]interface{})
	overrides := s["overrides"].([]interface{})
	if len(overrides) != 1 {
		t.Errorf("Expected 1 override, got %d", len(overrides))
	}
}

func TestWeekHandler_GetMyWeek_WrongUser(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk14coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk14user@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	_, otherToken := testutil.CreateTestUser(t, queries, "wk14other@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
		},
	})
	app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken))

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/user/programs/%s/weeks/1", programID), nil, otherToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403, got %d", resp.StatusCode)
	}
}

// An override that changes the row count without resending the arrays it
// invalidates would leave rows with no load, so it is rejected on write rather
// than silently prescribing bodyweight to the athlete.
func TestWeekHandler_RejectsOverrideResizingGridWithoutArrays(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk9coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk9user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID, itemID := createTestPerRepCoachTraining(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 1,
				"overrides": []map[string]interface{}{
					{
						"item_id":   itemID,
						"overrides": map[string]interface{}{"reps": 8},
					},
				},
			},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

// The same override is accepted when it carries the arrays for the new layout.
func TestWeekHandler_AcceptsOverrideResizingGridWithArrays(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk10coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk10user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID, itemID := createTestPerRepCoachTraining(t, coachToken, app)

	loads := make([]map[string]interface{}, 0, 8)
	for i := 0; i < 8; i++ {
		loads = append(loads, map[string]interface{}{"value": 20 + i, "unit": "kg"})
	}
	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 1,
				"overrides": []map[string]interface{}{
					{
						"item_id": itemID,
						"overrides": map[string]interface{}{
							"reps":          8,
							"loads":         loads,
							"edge_sizes_mm": []interface{}{20, 20, 20, 20, 18, 18, 18, 18},
						},
					},
				},
			},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}
}

// Two overrides in one payload may name the same item, so each is checked on its
// own rather than the last one standing for both: an invalid first override is
// refused even when a valid second one follows it on that item.
func TestWeekHandler_RejectsFirstOfTwoOverridesOnTheSameItem(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk17coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk17user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID, itemID := createTestPerRepCoachTraining(t, coachToken, app)

	loads := make([]map[string]interface{}, 0, 8)
	for i := 0; i < 8; i++ {
		loads = append(loads, map[string]interface{}{"value": 20 + i, "unit": "kg"})
	}
	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 1,
				"overrides": []map[string]interface{}{
					{
						"item_id":   itemID,
						"overrides": map[string]interface{}{"reps": 8},
					},
					{
						"item_id": itemID,
						"overrides": map[string]interface{}{
							"reps":          8,
							"loads":         loads,
							"edge_sizes_mm": []interface{}{20, 20, 20, 20, 18, 18, 18, 18},
						},
					},
				},
			},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

// An override carries loads through the same assessment checks as the item it
// targets, so a session cannot store a reference the item itself would reject.
func TestWeekHandler_RejectsOverrideWithUnreferencedAssessmentLoad(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk11coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk11user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID, itemID := createTestPerRepCoachTraining(t, coachToken, app)

	loads := []map[string]interface{}{{"value": 80, "unit": "percent_assessment"}}
	for i := 0; i < 5; i++ {
		loads = append(loads, map[string]interface{}{"value": 10, "unit": "kg"})
	}
	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 1,
				"overrides": []map[string]interface{}{
					{
						"item_id":   itemID,
						"overrides": map[string]interface{}{"loads": loads},
					},
				},
			},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected %d for an override load missing its assessment reference, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

// A per-rep hangboard item, so an override changing reps changes the row count.
func createTestPerRepCoachTraining(t *testing.T, coachToken string, app *fiber.App) (trainingID, itemID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Per-rep Hangboard",
		"items": []map[string]interface{}{
			{
				"type":             "repeater",
				"cycles":           1,
				"reps":             6,
				"worktime_seconds": 7,
				"rest_seconds":     3,
				"hand":             "both",
				"granularity":      "rep",
				"edge_sizes_mm":    []interface{}{20, 20, 20, 18, 18, 18},
				"loads": []map[string]interface{}{
					{"value": 10, "unit": "kg"},
					{"value": 11, "unit": "kg"},
					{"value": 12, "unit": "kg"},
					{"value": 13, "unit": "kg"},
					{"value": 14, "unit": "kg"},
					{"value": 15, "unit": "kg"},
				},
			},
		},
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/trainings", body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create coach training: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating coach training, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	trainingID = result["id"].(string)
	itemID = result["items"].([]interface{})[0].(map[string]interface{})["id"].(string)
	return trainingID, itemID
}

func upsertWeekSessions(t *testing.T, app *fiber.App, coachToken, userID, programID string, weekNum int, week map[string]interface{}) map[string]interface{} {
	t.Helper()
	body, _ := json.Marshal(week)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, weekNum), body, coachToken))
	if err != nil {
		t.Fatalf("Failed to upsert week: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 upserting week, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func upsertWeekStatus(t *testing.T, app *fiber.App, coachToken, userID, programID string, weekNum int, week map[string]interface{}) int {
	t.Helper()
	body, _ := json.Marshal(week)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, weekNum), body, coachToken))
	if err != nil {
		t.Fatalf("Failed to upsert week: %v", err)
	}
	return resp.StatusCode
}

// upsertWeekError returns the status and the error message, so a test can pin
// the wording rather than only the status. Overrides rejected for an item the
// session's training does not hold must stay indistinguishable from each other.
func upsertWeekError(t *testing.T, app *fiber.App, coachToken, userID, programID string, weekNum int, week map[string]interface{}) (int, string) {
	t.Helper()
	body, _ := json.Marshal(week)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, weekNum), body, coachToken))
	if err != nil {
		t.Fatalf("Failed to upsert week: %v", err)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	message, _ := result["error"].(string)
	return resp.StatusCode, message
}

func getWeek(t *testing.T, app *fiber.App, coachToken, userID, programID string, weekNum int) map[string]interface{} {
	t.Helper()
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, weekURL(userID, programID, weekNum), nil, coachToken))
	if err != nil {
		t.Fatalf("Failed to get week: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 getting week, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func weekSessionIDs(week map[string]interface{}) []string {
	sessions := week["sessions"].([]interface{})
	ids := make([]string, 0, len(sessions))
	for _, s := range sessions {
		ids = append(ids, s.(map[string]interface{})["id"].(string))
	}
	return ids
}

func TestWeekHandler_UpsertWeek_KeepsSessionIDsSentBack(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkidcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wkiduser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
			{"training_id": trainingID, "times_per_week": 2},
		},
	})
	originalIDs := weekSessionIDs(created)

	updated := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"notes": "Typo fixed",
		"sessions": []map[string]interface{}{
			{"id": originalIDs[0], "training_id": trainingID, "day_of_week": 0},
			{"id": originalIDs[1], "training_id": trainingID, "times_per_week": 2},
		},
	})

	updatedIDs := weekSessionIDs(updated)
	if len(updatedIDs) != 2 {
		t.Fatalf("Expected 2 sessions after edit, got %d", len(updatedIDs))
	}
	for i, id := range originalIDs {
		if updatedIDs[i] != id {
			t.Errorf("Expected session %d to keep id %s, got %s", i, id, updatedIDs[i])
		}
	}
	if updated["notes"] != "Typo fixed" {
		t.Errorf("Expected updated notes, got %v", updated["notes"])
	}
}

// The order of the sessions inside a day is the coach's, not an accident of how
// the rows were written. The payload order is the only thing carrying it, so the
// position it becomes has to survive both the write and a later read.
func TestWeekHandler_UpsertWeek_KeepsTheOrderOfSessionsInADay(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkordercoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wkorderuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	firstTrainingID := createTestCoachTraining(t, coachToken, app)
	secondTrainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": firstTrainingID, "day_of_week": 2},
			{"training_id": secondTrainingID, "day_of_week": 2},
		},
	})
	ids := weekSessionIDs(created)
	assertSessionOrder(t, created, "on create", ids[0], ids[1])

	swapped := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": ids[1], "training_id": secondTrainingID, "day_of_week": 2},
			{"id": ids[0], "training_id": firstTrainingID, "day_of_week": 2},
		},
	})
	assertSessionOrder(t, swapped, "after the swap", ids[1], ids[0])
	assertSessionOrder(t, getWeek(t, app, coachToken, userID, programID, 1), "on read back", ids[1], ids[0])
}

// Order means both the order the sessions come back in and the positions they
// carry, since a client is free to sort on either.
func assertSessionOrder(t *testing.T, week map[string]interface{}, stage string, wantIDs ...string) {
	t.Helper()
	sessions := week["sessions"].([]interface{})
	if len(sessions) != len(wantIDs) {
		t.Fatalf("Expected %d sessions %s, got %d", len(wantIDs), stage, len(sessions))
	}
	for i, wantID := range wantIDs {
		session := sessions[i].(map[string]interface{})
		if session["id"] != wantID {
			t.Errorf("Expected session %s at index %d %s, got %s", wantID, i, stage, session["id"])
		}
		if session["position"] != float64(i) {
			t.Errorf("Expected position %d for session at index %d %s, got %v", i, i, stage, session["position"])
		}
	}
}

func TestWeekHandler_UpsertWeek_KeepsPlayedSessionLink(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wklinkcoach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "wklinkuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:  handler.NewSessionHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	week := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	programSessionID := weekSessionIDs(week)[0]

	sessionBody, _ := json.Marshal(map[string]interface{}{
		"name":               "Prescribed hangboard",
		"notes":              "",
		"activity":           0,
		"origin":             "played",
		"duration":           600,
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", sessionBody, userToken))
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating session, got %d", resp.StatusCode)
	}
	var playedSession map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&playedSession)
	playedSessionID := playedSession["id"].(string)

	upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"notes": "Coach fixed a typo",
		"sessions": []map[string]interface{}{
			{"id": programSessionID, "training_id": trainingID, "day_of_week": 0},
		},
	})

	resp, err = app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/sessions/"+playedSessionID, nil, userToken))
	if err != nil {
		t.Fatalf("Failed to fetch session: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 fetching session, got %d", resp.StatusCode)
	}
	var reloaded map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&reloaded)
	session := reloaded["session"].(map[string]interface{})
	if session["program_session_id"] != programSessionID {
		t.Errorf("Expected played session to keep program_session_id %s, got %v", programSessionID, session["program_session_id"])
	}
}

func TestWeekHandler_UpsertWeek_DeletesSessionsMissingFromPayload(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkdropcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wkdropuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
			{"training_id": trainingID, "day_of_week": 2},
			{"training_id": trainingID, "day_of_week": 4},
		},
	})
	originalIDs := weekSessionIDs(created)

	updated := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": originalIDs[0], "training_id": trainingID, "day_of_week": 0},
			{"id": originalIDs[2], "training_id": trainingID, "day_of_week": 4},
			{"training_id": trainingID, "day_of_week": 6},
		},
	})

	updatedIDs := weekSessionIDs(updated)
	if len(updatedIDs) != 3 {
		t.Fatalf("Expected 3 sessions, got %d", len(updatedIDs))
	}
	if updatedIDs[0] != originalIDs[0] || updatedIDs[1] != originalIDs[2] {
		t.Errorf("Expected kept sessions to keep their ids, got %v from %v", updatedIDs, originalIDs)
	}
	if updatedIDs[2] == originalIDs[1] {
		t.Errorf("Expected the added session to be a new row, got the removed session id %s", originalIDs[1])
	}
}

func TestWeekHandler_UpsertWeek_KeepsSessionIDWhenScheduleChanges(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkmovecoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wkmoveuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	sessionID := weekSessionIDs(created)[0]

	updated := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": sessionID, "training_id": trainingID, "is_everyday": true, "notes": "Every day now"},
		},
	})

	session := updated["sessions"].([]interface{})[0].(map[string]interface{})
	if session["id"] != sessionID {
		t.Errorf("Expected id %s to survive a schedule change, got %v", sessionID, session["id"])
	}
	if session["is_everyday"] != true {
		t.Errorf("Expected is_everyday true, got %v", session["is_everyday"])
	}
	if _, ok := session["day_of_week"]; ok {
		t.Errorf("Expected day_of_week to be cleared, got %v", session["day_of_week"])
	}
	if session["notes"] != "Every day now" {
		t.Errorf("Expected updated notes, got %v", session["notes"])
	}
}

func TestWeekHandler_UpsertWeek_DropsOverridesMissingFromPayload(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkovrcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wkovruser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 1,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 6}},
				},
			},
		},
	})
	sessionID := weekSessionIDs(created)[0]

	updated := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": sessionID, "training_id": trainingID, "day_of_week": 1},
		},
	})

	session := updated["sessions"].([]interface{})[0].(map[string]interface{})
	if session["id"] != sessionID {
		t.Errorf("Expected id %s to be kept, got %v", sessionID, session["id"])
	}
	overrides := session["overrides"].([]interface{})
	if len(overrides) != 0 {
		t.Errorf("Expected overrides to be dropped, got %v", overrides)
	}
}

func TestWeekHandler_UpsertWeek_RejectsForeignSessionID(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkforeigncoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wkforeignuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	weekOne := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	otherWeekSessionID := weekSessionIDs(weekOne)[0]

	status := upsertWeekStatus(t, app, coachToken, userID, programID, 2, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": otherWeekSessionID, "training_id": trainingID, "day_of_week": 0},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for a session id from another week, got %d", status)
	}

	stillThere := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": otherWeekSessionID, "training_id": trainingID, "day_of_week": 0},
		},
	})
	if weekSessionIDs(stillThere)[0] != otherWeekSessionID {
		t.Errorf("Expected the rejected upsert to leave week 1 untouched")
	}
}

func TestWeekHandler_UpsertWeek_RejectsSessionIDFromAnotherCoach(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachAID, coachAToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkxcoacha@test.com")
	userAID, _ := testutil.CreateTestUser(t, queries, "wkxusera@test.com")
	enrollUserDirect(t, pool, coachAID, userAID)

	coachBID, coachBToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkxcoachb@test.com")
	userBID, _ := testutil.CreateTestUser(t, queries, "wkxuserb@test.com")
	enrollUserDirect(t, pool, coachBID, userBID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programA := createTestProgram(t, coachAToken, userAID, app)
	trainingA := createTestCoachTraining(t, coachAToken, app)
	weekA := upsertWeekSessions(t, app, coachAToken, userAID, programA, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingA, "day_of_week": 0}},
	})
	sessionAID := weekSessionIDs(weekA)[0]

	programB := createTestProgram(t, coachBToken, userBID, app)
	trainingB := createTestCoachTraining(t, coachBToken, app)

	status := upsertWeekStatus(t, app, coachBToken, userBID, programB, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": sessionAID, "training_id": trainingB, "day_of_week": 0},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for a session id owned by another coach, got %d", status)
	}

	untouched := upsertWeekSessions(t, app, coachAToken, userAID, programA, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": sessionAID, "training_id": trainingA, "day_of_week": 0},
		},
	})
	session := untouched["sessions"].([]interface{})[0].(map[string]interface{})
	if session["id"] != sessionAID {
		t.Errorf("Expected coach A's session to survive, got %v", session["id"])
	}
	if session["training_id"] != trainingA {
		t.Errorf("Expected coach A's training to be untouched, got %v", session["training_id"])
	}
}

// Scheduling another coach's training would otherwise be served straight back
// by the week read, which joins trainings unconditionally, and would re-admit
// that coach's items as legitimate override targets.
func TestWeekHandler_UpsertWeek_RejectsTrainingFromAnotherCoach(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachAID, coachAToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkxtrcoacha@test.com")
	userAID, _ := testutil.CreateTestUser(t, queries, "wkxtrusera@test.com")
	enrollUserDirect(t, pool, coachAID, userAID)

	coachBID, coachBToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkxtrcoachb@test.com")
	userBID, _ := testutil.CreateTestUser(t, queries, "wkxtruserb@test.com")
	enrollUserDirect(t, pool, coachBID, userBID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	trainingA, _ := createTestCoachTrainingWithItems(t, coachAToken, app)
	programB := createTestProgram(t, coachBToken, userBID, app)

	status, message := upsertWeekError(t, app, coachBToken, userBID, programB, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingA, "day_of_week": 0},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for a training owned by another coach, got %d", status)
	}
	if message != unknownTrainingError {
		t.Errorf("Expected %q, got %q", unknownTrainingError, message)
	}

	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, weekURL(userBID, programB, 1), nil, coachBToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected the rejected upsert to leave no week behind, got %d", resp.StatusCode)
	}
}

// A training that exists nowhere answers exactly as one owned by another coach,
// so the endpoint cannot be used to tell a real training uuid from a random one.
func TestWeekHandler_UpsertWeek_RejectsUnknownTrainingIdentically(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wknotrcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wknotruser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)

	status, message := upsertWeekError(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": uuid.NewString(), "day_of_week": 0},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for a training that exists nowhere, got %d", status)
	}
	if message != unknownTrainingError {
		t.Errorf("Expected %q, got %q", unknownTrainingError, message)
	}
}

func TestWeekHandler_UpsertWeek_RejectsOverrideItemFromAnotherCoach(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachAID, coachAToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkxitemcoacha@test.com")
	userAID, _ := testutil.CreateTestUser(t, queries, "wkxitemusera@test.com")
	enrollUserDirect(t, pool, coachAID, userAID)

	coachBID, coachBToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkxitemcoachb@test.com")
	userBID, _ := testutil.CreateTestUser(t, queries, "wkxitemuserb@test.com")
	enrollUserDirect(t, pool, coachBID, userBID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	_, itemA := createTestCoachTrainingWithItems(t, coachAToken, app)

	programB := createTestProgram(t, coachBToken, userBID, app)
	trainingB, _ := createTestCoachTrainingWithItems(t, coachBToken, app)
	upsertWeekSessions(t, app, coachBToken, userBID, programB, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingB, "day_of_week": 0}},
	})

	status, message := upsertWeekError(t, app, coachBToken, userBID, programB, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingB,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": itemA, "overrides": map[string]interface{}{"reps": 8}},
				},
			},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for an item id owned by another coach, got %d", status)
	}
	if message != unknownOverrideItemError {
		t.Errorf("Expected %q, got %q", unknownOverrideItemError, message)
	}

	week := getWeek(t, app, coachBToken, userBID, programB, 1)
	session := week["sessions"].([]interface{})[0].(map[string]interface{})
	if overrides := session["overrides"].([]interface{}); len(overrides) != 0 {
		t.Errorf("Expected no override to be stored, got %v", overrides)
	}
}

func TestWeekHandler_UpsertWeek_RejectsOverrideItemFromAnotherTraining(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkotheritemcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wkotheritemuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	scheduledTraining, _ := createTestCoachTrainingWithItems(t, coachToken, app)
	_, unrelatedItem := createTestCoachTrainingWithItems(t, coachToken, app)

	status, message := upsertWeekError(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": scheduledTraining,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": unrelatedItem, "overrides": map[string]interface{}{"reps": 8}},
				},
			},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for an item id from an unrelated training, got %d", status)
	}
	if message != unknownOverrideItemError {
		t.Errorf("Expected %q, got %q", unknownOverrideItemError, message)
	}
}

// An item id the session's training does not hold answers the same way whether
// it exists or not, so the endpoint cannot be used to tell a real item uuid from
// a random one, nor to learn an item's type from the validation wording.
func TestWeekHandler_UpsertWeek_RejectsUnknownOverrideItemIdentically(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wknoitemcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wknoitemuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	scheduledTraining, _ := createTestCoachTrainingWithItems(t, coachToken, app)

	status, message := upsertWeekError(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": scheduledTraining,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": uuid.NewString(), "overrides": map[string]interface{}{"reps": 8}},
				},
			},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for an item id that exists nowhere, got %d", status)
	}
	if message != unknownOverrideItemError {
		t.Errorf("Expected %q, got %q", unknownOverrideItemError, message)
	}
}

func TestWeekHandler_UpsertWeek_RejectsMalformedAndDuplicateSessionIDs(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wkbadidcoach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wkbadiduser@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	sessionID := weekSessionIDs(created)[0]

	cases := []struct {
		name     string
		sessions []map[string]interface{}
	}{
		{"malformed id", []map[string]interface{}{
			{"id": "not-a-uuid", "training_id": trainingID, "day_of_week": 0},
		}},
		{"duplicate id", []map[string]interface{}{
			{"id": sessionID, "training_id": trainingID, "day_of_week": 0},
			{"id": sessionID, "training_id": trainingID, "day_of_week": 2},
		}},
		{"duplicate id in another spelling", []map[string]interface{}{
			{"id": sessionID, "training_id": trainingID, "day_of_week": 0},
			{"id": strings.ToUpper(sessionID), "training_id": trainingID, "day_of_week": 2},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := upsertWeekStatus(t, app, coachToken, userID, programID, 1, map[string]interface{}{"sessions": tc.sessions})
			if status != fiber.StatusBadRequest {
				t.Errorf("Expected 400, got %d", status)
			}
		})
	}
}

func playProgramSession(t *testing.T, app *fiber.App, userToken, trainingID, programSessionID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"name":               "Prescribed hangboard",
		"notes":              "",
		"activity":           0,
		"origin":             "played",
		"duration":           600,
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, userToken))
	if err != nil {
		t.Fatalf("Failed to play session: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 playing session, got %d", resp.StatusCode)
	}
}

func weekSessionLocks(week map[string]interface{}) []bool {
	sessions := week["sessions"].([]interface{})
	locks := make([]bool, 0, len(sessions))
	for _, s := range sessions {
		locks = append(locks, s.(map[string]interface{})["is_locked"].(bool))
	}
	return locks
}

func setupFrozenSessionApp(t *testing.T, prefix string) (app *fiber.App, coachToken, userToken, userID, programID string) {
	t.Helper()
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, prefix+"coach@test.com")
	userID, userToken = testutil.CreateTestUser(t, queries, prefix+"user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app = testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:  handler.NewSessionHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})
	programID = createTestProgram(t, coachToken, userID, app)
	return app, coachToken, userToken, userID, programID
}

func TestWeekHandler_GetWeek_ReportsPlayedSessionAsLocked(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wklock")
	trainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
			{"training_id": trainingID, "day_of_week": 2},
		},
	})
	if locks := weekSessionLocks(created); locks[0] || locks[1] {
		t.Fatalf("Expected no session locked before any is played, got %v", locks)
	}
	sessionIDs := weekSessionIDs(created)

	playProgramSession(t, app, userToken, trainingID, sessionIDs[0])

	week := getWeek(t, app, coachToken, userID, programID, 1)

	locks := weekSessionLocks(week)
	if !locks[0] {
		t.Errorf("Expected the played session to be locked")
	}
	if locks[1] {
		t.Errorf("Expected the unplayed session to stay unlocked")
	}
}

func TestWeekHandler_UpsertWeek_RefusesTrainingSwapOnPlayedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wkfrozenswap")
	trainingID := createTestCoachTraining(t, coachToken, app)
	otherTrainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	sessionID := weekSessionIDs(created)[0]
	playProgramSession(t, app, userToken, trainingID, sessionID)

	status := upsertWeekStatus(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": sessionID, "training_id": otherTrainingID, "day_of_week": 0},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 swapping the training of a played session, got %d", status)
	}
}

func TestWeekHandler_UpsertWeek_RefusesRemovingPlayedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wkfrozendrop")
	trainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
			{"training_id": trainingID, "day_of_week": 2},
		},
	})
	sessionIDs := weekSessionIDs(created)
	playProgramSession(t, app, userToken, trainingID, sessionIDs[0])

	status := upsertWeekStatus(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": sessionIDs[1], "training_id": trainingID, "day_of_week": 2},
		},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 dropping a played session from the week, got %d", status)
	}

	week := getWeek(t, app, coachToken, userID, programID, 1)
	if len(weekSessionIDs(week)) != 2 {
		t.Errorf("Expected the refused save to leave both sessions in place, got %v", week["sessions"])
	}
}

func TestWeekHandler_UpsertWeek_RefusesOverrideEditOnPlayedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wkfrozenov")
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
				},
			},
		},
	})
	sessionID := weekSessionIDs(created)[0]
	playProgramSession(t, app, userToken, trainingID, sessionID)

	cases := []struct {
		name      string
		overrides []map[string]interface{}
	}{
		{"changed value", []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{"reps": 8}},
		}},
		{"dropped override", []map[string]interface{}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := upsertWeekStatus(t, app, coachToken, userID, programID, 1, map[string]interface{}{
				"sessions": []map[string]interface{}{
					{"id": sessionID, "training_id": trainingID, "day_of_week": 0, "overrides": tc.overrides},
				},
			})
			if status != fiber.StatusBadRequest {
				t.Errorf("Expected 400, got %d", status)
			}
		})
	}
}

// Nothing about the schedule or the notes changes what was prescribed, so a
// played session may still be moved around the week.
func TestWeekHandler_UpsertWeek_AllowsReschedulingPlayedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wkfrozenmove")
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
				},
			},
		},
	})
	sessionID := weekSessionIDs(created)[0]
	playProgramSession(t, app, userToken, trainingID, sessionID)

	updated := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"id":          sessionID,
				"training_id": trainingID,
				"day_of_week": 3,
				"notes":       "Moved to Thursday",
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
				},
			},
		},
	})

	session := updated["sessions"].([]interface{})[0].(map[string]interface{})
	if session["day_of_week"] != float64(3) {
		t.Errorf("Expected day_of_week 3, got %v", session["day_of_week"])
	}
	if session["notes"] != "Moved to Thursday" {
		t.Errorf("Expected the notes to be editable, got %v", session["notes"])
	}
	if session["is_locked"] != true {
		t.Errorf("Expected the session to stay locked, got %v", session["is_locked"])
	}
}

// The mode columns and the ordering say when the session happens, not what was
// prescribed, so all of them stay editable once it has been played.
func TestWeekHandler_UpsertWeek_AllowsModeAndPositionChangeOnPlayedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wkfrozenmode")
	trainingID := createTestCoachTraining(t, coachToken, app)
	otherTrainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
			{"training_id": otherTrainingID, "day_of_week": 1},
		},
	})
	ids := weekSessionIDs(created)
	playProgramSession(t, app, userToken, trainingID, ids[0])

	// Swap the order and move the played one off a fixed day onto a frequency.
	updated := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": ids[1], "training_id": otherTrainingID, "day_of_week": 1},
			{"id": ids[0], "training_id": trainingID, "times_per_week": 3},
		},
	})

	sessions := updated["sessions"].([]interface{})
	played := sessions[1].(map[string]interface{})
	if played["id"] != ids[0] {
		t.Fatalf("Expected the played session to be reordered last, got %v", weekSessionIDs(updated))
	}
	if played["times_per_week"] != float64(3) {
		t.Errorf("Expected times_per_week 3, got %v", played["times_per_week"])
	}
	if _, hasDay := played["day_of_week"]; hasDay {
		t.Errorf("Expected day_of_week cleared, got %v", played["day_of_week"])
	}
	if played["position"] != float64(1) {
		t.Errorf("Expected position 1, got %v", played["position"])
	}
	if played["is_locked"] != true {
		t.Errorf("Expected the session to stay locked, got %v", played["is_locked"])
	}
}

// The freeze is per row. A played session in the week must not stop the coach
// editing the sessions next to it.
func TestWeekHandler_UpsertWeek_FreezesOnlyThePlayedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wkfrozenrow")
	trainingID, itemID := createTestCoachTrainingWithItems(t, coachToken, app)
	otherTrainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
				},
			},
			{
				"training_id": trainingID,
				"day_of_week": 2,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
				},
			},
		},
	})
	ids := weekSessionIDs(created)
	playProgramSession(t, app, userToken, trainingID, ids[0])

	// Swap the training and the overrides of the unplayed one, and drop it from
	// the week afterwards. Neither is refused.
	updated := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"id":          ids[0],
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
				},
			},
			{"id": ids[1], "training_id": otherTrainingID, "day_of_week": 2},
		},
	})
	if got := updated["sessions"].([]interface{})[1].(map[string]interface{})["training_id"]; got != otherTrainingID {
		t.Errorf("Expected the unplayed session to accept a training swap, got %v", got)
	}

	removed := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"id":          ids[0],
				"training_id": trainingID,
				"day_of_week": 0,
				"overrides": []map[string]interface{}{
					{"item_id": itemID, "overrides": map[string]interface{}{"reps": 5}},
				},
			},
		},
	})
	if got := weekSessionIDs(removed); len(got) != 1 || got[0] != ids[0] {
		t.Errorf("Expected the unplayed session to be removable, got %v", got)
	}
}

// A coach slot with nothing to step through is completed by hand, so a logged
// session may answer a prescription. It still must not freeze the week: it was
// typed in after the fact, and locking on it would hand an athlete a way to
// pin their coach's week to a session they never played.
func TestSessionHandler_CreateSession_AcceptsProgramSessionOnLoggedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wkloggedlink")
	trainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	sessionID := weekSessionIDs(created)[0]

	body, _ := json.Marshal(map[string]interface{}{
		"name":               "Hand entered",
		"notes":              "",
		"activity":           0,
		"origin":             "logged",
		"duration":           600,
		"training_id":        trainingID,
		"program_session_id": sessionID,
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, userToken))
	if err != nil {
		t.Fatalf("Failed to post session: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 for a logged session carrying a program session, got %d", resp.StatusCode)
	}

	var session map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session: %v", err)
	}
	// The link is what program completion matches on, so it has to come back.
	if got := session["program_session_id"]; got != sessionID {
		t.Errorf("Expected the logged session to keep the program session link, got %v", got)
	}
	if got := session["origin"]; got != "logged" {
		t.Errorf("Expected the session to stay logged, got %v", got)
	}

	if locks := weekSessionLocks(getWeek(t, app, coachToken, userID, programID, 1)); locks[0] {
		t.Errorf("Expected a logged session not to lock the week")
	}
}

// The coach must stay free to edit a week an athlete only logged by hand, which
// is the half of the rule the lock check enforces rather than the constraint.
func TestWeekHandler_UpsertWeek_EditsWeekHoldingLoggedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wkloggededit")
	trainingID := createTestCoachTraining(t, coachToken, app)
	otherTrainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	sessionID := weekSessionIDs(created)[0]

	body, _ := json.Marshal(map[string]interface{}{
		"name":               "Went climbing",
		"notes":              "",
		"activity":           0,
		"origin":             "logged",
		"duration":           600,
		"training_id":        trainingID,
		"program_session_id": sessionID,
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, userToken))
	if err != nil {
		t.Fatalf("Failed to post session: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 logging the session, got %d", resp.StatusCode)
	}

	// Swapping the training would be refused outright on a played session.
	edited := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"id": sessionID, "training_id": otherTrainingID, "day_of_week": 2},
		},
	})
	if got := weekSessionIDs(edited); len(got) != 1 || got[0] != sessionID {
		t.Errorf("Expected the slot to keep its id through the edit, got %v", got)
	}
}

// Deleting the week cascades to its sessions and the sessions FK nulls on
// delete, so it is the removal the week PUT refuses by another route.
func TestWeekHandler_DeleteWeek_RefusesWeekHoldingPlayedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wkdelfrozen")
	trainingID := createTestCoachTraining(t, coachToken, app)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	sessionID := weekSessionIDs(created)[0]
	playProgramSession(t, app, userToken, trainingID, sessionID)

	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodDelete, weekURL(userID, programID, 1), nil, coachToken))
	if err != nil {
		t.Fatalf("Failed to delete week: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 deleting a week with a played session, got %d", resp.StatusCode)
	}

	if got := weekSessionIDs(getWeek(t, app, coachToken, userID, programID, 1)); len(got) != 1 || got[0] != sessionID {
		t.Errorf("Expected the refused delete to leave the session in place, got %v", got)
	}

	respProgram, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodDelete, "/api/coach/clients/"+userID+"/programs/"+programID, nil, coachToken))
	if err != nil {
		t.Fatalf("Failed to delete program: %v", err)
	}
	if respProgram.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 deleting a program holding a played session, got %d", respProgram.StatusCode)
	}
}

func TestWeekHandler_DeleteWeek_AllowsWeekWithoutPlayedSession(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, userToken, userID, programID := setupFrozenSessionApp(t, "wkdelfree")
	trainingID := createTestCoachTraining(t, coachToken, app)

	upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	created := upsertWeekSessions(t, app, coachToken, userID, programID, 2, map[string]interface{}{
		"sessions": []map[string]interface{}{{"training_id": trainingID, "day_of_week": 0}},
	})
	// A played session in week 2 must not hold week 1 hostage.
	playProgramSession(t, app, userToken, trainingID, weekSessionIDs(created)[0])

	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodDelete, weekURL(userID, programID, 1), nil, coachToken))
	if err != nil {
		t.Fatalf("Failed to delete week: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 deleting a week with no played session, got %d", resp.StatusCode)
	}
}

// The override path reaches the repeat columns without going through the item
// validator, so a week cannot be the back door that puts a rep or cycle count on
// a hangboard_rep the athlete's app would expand to a single hang anyway.
func TestWeekHandler_RejectsOverrideRepeatFieldsOnHangboardRep(t *testing.T) {
	for index, field := range []string{"reps", "cycles", "cycle_rest_seconds"} {
		t.Run(field, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "test-secret-key")
			pool, queries := testutil.SetupTestDB(t)
			defer testutil.CleanupTestDB(t, pool)

			coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, fmt.Sprintf("wk15%dcoach@test.com", index))
			userID, _ := testutil.CreateTestUser(t, queries, fmt.Sprintf("wk15%duser@test.com", index))
			enrollUserDirect(t, pool, coachID, userID)

			app := testutil.SetupFiberApp(testutil.HandlerConfig{
				TrainingHandler: handler.NewTrainingHandler(queries, pool),
				ProgramHandler:  handler.NewProgramHandler(queries, pool),
			})

			programID := createTestProgram(t, coachToken, userID, app)
			trainingID, itemID := createTestSingleHangCoachTraining(t, coachToken, app)

			body, _ := json.Marshal(map[string]interface{}{
				"sessions": []map[string]interface{}{
					{
						"training_id": trainingID,
						"day_of_week": 1,
						"overrides": []map[string]interface{}{
							{
								"item_id":   itemID,
								"overrides": map[string]interface{}{field: 4},
							},
						},
					},
				},
			})

			req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("Expected %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
			}
			var result map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&result)
			message := fmt.Sprint(result["error"])
			if !strings.Contains(message, "repeater") {
				t.Errorf("Expected the error to name the repeater, got %v", message)
			}
			if !strings.Contains(message, field) {
				t.Errorf("Expected the error to name %s, got %v", field, message)
			}
		})
	}
}

// The other fields of the same override are still accepted on a single hang:
// only the rep count is refused.
func TestWeekHandler_AcceptsOverrideLoadOnHangboardRep(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "wk16coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "wk16user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
	})

	programID := createTestProgram(t, coachToken, userID, app)
	trainingID, itemID := createTestSingleHangCoachTraining(t, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{
				"training_id": trainingID,
				"day_of_week": 1,
				"overrides": []map[string]interface{}{
					{
						"item_id": itemID,
						"overrides": map[string]interface{}{
							"loads": []map[string]interface{}{{"value": 25, "unit": "kg"}},
						},
					},
				},
			},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPut, weekURL(userID, programID, 1), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}
}

func createTestSingleHangCoachTraining(t *testing.T, coachToken string, app *fiber.App) (trainingID, itemID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Single Hang",
		"items": []map[string]interface{}{
			{
				"type":             "hangboard_rep",
				"worktime_seconds": 7,
				"rest_seconds":     60,
				"hand":             "both",
				"granularity":      "uniform",
				"edge_sizes_mm":    []interface{}{20},
				"loads":            []map[string]interface{}{{"value": 10, "unit": "kg"}},
			},
		},
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/trainings", body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create coach training: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating coach training, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	trainingID = result["id"].(string)
	itemID = result["items"].([]interface{})[0].(map[string]interface{})["id"].(string)
	return trainingID, itemID
}
