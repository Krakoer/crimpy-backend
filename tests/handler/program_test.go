package handler_test

import (
	"context"
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

func enrollUserDirect(t *testing.T, pool *pgxpool.Pool, coachID, userID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		"INSERT INTO coach_enrollments (coach_id, user_id) VALUES ($1, $2)", coachID, userID)
	if err != nil {
		t.Fatalf("Failed to enroll user: %v", err)
	}
}

func createTestCoachTrainingForProgram(t *testing.T, pool *pgxpool.Pool, queries interface{}, coachToken string, app *fiber.App) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Test Training Template",
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/coach/trainings", body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create coach training: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating coach training, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result["id"].(string)
}

func TestProgramHandler_Create_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog1coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "prog1user@test.com")
	_ = userToken
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
		ProgramHandler:       handler.NewProgramHandler(queries, pool),
	})

	trainingID := createTestCoachTrainingForProgram(t, pool, queries, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"name":           "Test Program",
		"objective":      "Get stronger",
		"start_date":     "2026-06-01",
		"duration_weeks": 6,
		"slots": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
			{"training_id": trainingID, "times_per_week": 2},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected 201, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if result["name"] != "Test Program" {
		t.Errorf("Expected name 'Test Program', got %v", result["name"])
	}
	slots, ok := result["slots"].([]interface{})
	if !ok || len(slots) != 2 {
		t.Errorf("Expected 2 slots, got %v", result["slots"])
	}
}

func TestProgramHandler_Create_NotCoach(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, userToken := testutil.CreateTestUser(t, queries, "prog2user@test.com")
	targetID, _ := testutil.CreateTestUser(t, queries, "prog2target@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"name":       "Test Program",
		"start_date": "2026-06-01",
		"slots":      []interface{}{},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", targetID), body, userToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403, got %d", resp.StatusCode)
	}
}

func TestProgramHandler_Create_ClientNotEnrolled(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog3coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog3user@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"name":       "Test Program",
		"start_date": "2026-06-01",
		"slots":      []interface{}{},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403, got %d", resp.StatusCode)
	}
}

func TestProgramHandler_Create_MissingName(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog4coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog4user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"start_date": "2026-06-01",
		"slots":      []interface{}{},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400, got %d", resp.StatusCode)
	}
}

func TestProgramHandler_Create_InvalidDate(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog5coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog5user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"name":       "Test Program",
		"start_date": "not-a-date",
		"slots":      []interface{}{},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400, got %d", resp.StatusCode)
	}
}

func TestProgramHandler_Create_SlotBothFields(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog6coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog6user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
		ProgramHandler:       handler.NewProgramHandler(queries, pool),
	})

	trainingID := createTestCoachTrainingForProgram(t, pool, queries, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"name":       "Test Program",
		"start_date": "2026-06-01",
		"slots": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0, "times_per_week": 2},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400, got %d", resp.StatusCode)
	}
}

func TestProgramHandler_Create_SlotNoField(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog7coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog7user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
		ProgramHandler:       handler.NewProgramHandler(queries, pool),
	})

	trainingID := createTestCoachTrainingForProgram(t, pool, queries, coachToken, app)

	body, _ := json.Marshal(map[string]interface{}{
		"name":       "Test Program",
		"start_date": "2026-06-01",
		"slots": []map[string]interface{}{
			{"training_id": trainingID},
		},
	})

	req := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400, got %d", resp.StatusCode)
	}
}

func TestProgramHandler_GetPrograms(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog8coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog8user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
		ProgramHandler:       handler.NewProgramHandler(queries, pool),
	})

	for i := 0; i < 2; i++ {
		body, _ := json.Marshal(map[string]interface{}{
			"name":       fmt.Sprintf("Program %d", i+1),
			"start_date": fmt.Sprintf("2026-0%d-01", i+6),
			"slots":      []interface{}{},
		})
		req := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), body, coachToken)
		app.Test(req)
	}

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/coach/clients/%s/programs", userID), nil, coachToken)
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
		t.Errorf("Expected 2 programs, got %d", len(result))
	}
}

func TestProgramHandler_GetProgram(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog9coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog9user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
		ProgramHandler:       handler.NewProgramHandler(queries, pool),
	})

	trainingID := createTestCoachTrainingForProgram(t, pool, queries, coachToken, app)

	createBody, _ := json.Marshal(map[string]interface{}{
		"name":       "My Program",
		"start_date": "2026-06-01",
		"slots": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
		},
	})
	createReq := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), createBody, coachToken)
	createResp, _ := app.Test(createReq)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	programID := created["id"].(string)

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/coach/clients/%s/programs/%s", userID, programID), nil, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	slots, ok := result["slots"].([]interface{})
	if !ok || len(slots) != 1 {
		t.Errorf("Expected 1 slot with training_title, got %v", result["slots"])
	}
	slot := slots[0].(map[string]interface{})
	if slot["training_title"] == nil || slot["training_title"] == "" {
		t.Errorf("Expected training_title in slot, got %v", slot)
	}
}

func TestProgramHandler_GetProgram_NotOwner(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog10coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog10user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	coach2ID, coach2Token := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog10coach2@test.com")
	user2ID, _ := testutil.CreateTestUser(t, queries, "prog10user2@test.com")
	enrollUserDirect(t, pool, coach2ID, user2ID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	createBody, _ := json.Marshal(map[string]interface{}{
		"name":       "My Program",
		"start_date": "2026-06-01",
		"slots":      []interface{}{},
	})
	createReq := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), createBody, coachToken)
	createResp, _ := app.Test(createReq)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	programID := created["id"].(string)

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/coach/clients/%s/programs/%s", user2ID, programID), nil, coach2Token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403, got %d", resp.StatusCode)
	}
}

func TestProgramHandler_Update(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog11coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog11user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		CoachTrainingHandler: handler.NewCoachTrainingHandler(queries, pool),
		ProgramHandler:       handler.NewProgramHandler(queries, pool),
	})

	trainingID := createTestCoachTrainingForProgram(t, pool, queries, coachToken, app)

	createBody, _ := json.Marshal(map[string]interface{}{
		"name":       "Original Name",
		"start_date": "2026-06-01",
		"slots": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
			{"training_id": trainingID, "day_of_week": 2},
		},
	})
	createReq := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), createBody, coachToken)
	createResp, _ := app.Test(createReq)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	programID := created["id"].(string)

	updateBody, _ := json.Marshal(map[string]interface{}{
		"name":       "Updated Name",
		"start_date": "2026-06-01",
		"slots": []map[string]interface{}{
			{"training_id": trainingID, "times_per_week": 3},
		},
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, fmt.Sprintf("/api/coach/clients/%s/programs/%s", userID, programID), updateBody, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["name"] != "Updated Name" {
		t.Errorf("Expected 'Updated Name', got %v", result["name"])
	}
	slots, _ := result["slots"].([]interface{})
	if len(slots) != 1 {
		t.Errorf("Expected 1 slot after update, got %d", len(slots))
	}
}

func TestProgramHandler_Delete(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog12coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog12user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	createBody, _ := json.Marshal(map[string]interface{}{
		"name":       "To Delete",
		"start_date": "2026-06-01",
		"slots":      []interface{}{},
	})
	createReq := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), createBody, coachToken)
	createResp, _ := app.Test(createReq)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	programID := created["id"].(string)

	delReq := testutil.NewJSONRequestWithAuth(http.MethodDelete, fmt.Sprintf("/api/coach/clients/%s/programs/%s", userID, programID), nil, coachToken)
	delResp, err := app.Test(delReq)
	if err != nil {
		t.Fatalf("Delete request failed: %v", err)
	}
	if delResp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200 on delete, got %d", delResp.StatusCode)
	}

	getReq := testutil.NewJSONRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/coach/clients/%s/programs/%s", userID, programID), nil, coachToken)
	getResp, _ := app.Test(getReq)
	if getResp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Expected 404 after delete, got %d", getResp.StatusCode)
	}
}

func TestProgramHandler_GetMyPrograms(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog13coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "prog13user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	createBody, _ := json.Marshal(map[string]interface{}{
		"name":       "My Program",
		"start_date": "2026-06-01",
		"slots":      []interface{}{},
	})
	createReq := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), createBody, coachToken)
	app.Test(createReq)

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/user/programs", nil, userToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result []interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 1 {
		t.Errorf("Expected 1 program, got %d", len(result))
	}
}

func TestProgramHandler_GetMyProgram(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog14coach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "prog14user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	createBody, _ := json.Marshal(map[string]interface{}{
		"name":       "My Program",
		"start_date": "2026-06-01",
		"slots":      []interface{}{},
	})
	createReq := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), createBody, coachToken)
	createResp, _ := app.Test(createReq)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	programID := created["id"].(string)

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/user/programs/%s", programID), nil, userToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

func TestProgramHandler_GetMyProgram_WrongUser(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "prog15coach@test.com")
	userID, _ := testutil.CreateTestUser(t, queries, "prog15user@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	_, otherToken := testutil.CreateTestUser(t, queries, "prog15other@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})

	createBody, _ := json.Marshal(map[string]interface{}{
		"name":       "My Program",
		"start_date": "2026-06-01",
		"slots":      []interface{}{},
	})
	createReq := testutil.NewJSONRequestWithAuth(http.MethodPost, fmt.Sprintf("/api/coach/clients/%s/programs", userID), createBody, coachToken)
	createResp, _ := app.Test(createReq)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	programID := created["id"].(string)

	req := testutil.NewJSONRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/user/programs/%s", programID), nil, otherToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403, got %d", resp.StatusCode)
	}
}
