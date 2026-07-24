package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func createTestCoachTrainingWithItems(t *testing.T, coachToken string, app *fiber.App) (trainingID, itemID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Hangboard Training",
		"items": []map[string]interface{}{
			{
				"type":                "hangboard_rep",
				"reps":                6,
				"hb_worktime_seconds": 7,
				"rest_seconds":        60,
				"both_hands":          true,
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
