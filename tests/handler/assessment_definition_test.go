package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"

	"crimpy/backend/internal/db"
)

// assessmentApp wires the handlers a custom assessment needs: the trainings it
// is run from, the definitions themselves and the results measured against them.
func assessmentApp(t *testing.T, pool *pgxpool.Pool, queries *db.Queries) *fiber.App {
	t.Helper()
	return testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler:             handler.NewTrainingHandler(queries, pool),
		AssessmentHandler:           handler.NewAssessmentHandler(queries),
		AssessmentDefinitionHandler: handler.NewAssessmentDefinitionHandler(queries, pool),
		SessionHandler:              handler.NewSessionHandler(queries, pool),
	})
}

func postJSON(t *testing.T, app *fiber.App, path, token string, payload map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, path, body, token))
	if err != nil {
		t.Fatalf("Request to %s failed: %v", path, err)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// createAssessmentTraining makes a training and declares it an assessment,
// returning the training id and the assessment id.
func createAssessmentTraining(t *testing.T, app *fiber.App, token, title, unit string, perHand bool) (string, string) {
	t.Helper()
	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": title,
		"items": []map[string]interface{}{{"type": "free", "comment": "to failure"}},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the assessment training, got %d: %v", status, training)
	}
	trainingID := training["id"].(string)

	status, definition := postJSON(t, app, "/api/assessment-definitions", token, map[string]interface{}{
		"training_id": trainingID,
		"label":       title,
		"prompt":      "How many did you do?",
		"unit":        unit,
		"per_hand":    perHand,
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the assessment, got %d: %v", status, definition)
	}
	return trainingID, definition["id"].(string)
}

func TestAssessmentDefinitions_ListsBuiltinsAndOwn(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessdefs1@test.com")
	app := assessmentApp(t, pool, queries)

	_, ownID := createAssessmentTraining(t, app, token, "Pull up pyramid", "repetitions", false)

	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/assessment-definitions", nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var list []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&list)

	byID := make(map[string]map[string]interface{}, len(list))
	for _, d := range list {
		byID[d["id"].(string)] = d
	}

	for _, builtin := range []string{
		testutil.BuiltinCriticalForceID,
		testutil.BuiltinMaxForceID,
		testutil.BuiltinEndurance60ID,
	} {
		found, ok := byID[builtin]
		if !ok {
			t.Fatalf("Expected the builtin %s in the list, got %v", builtin, list)
		}
		if found["is_builtin"] != true {
			t.Errorf("Expected %s flagged builtin, got %v", builtin, found["is_builtin"])
		}
	}

	own, ok := byID[ownID]
	if !ok {
		t.Fatalf("Expected the caller's own assessment in the list, got %v", list)
	}
	if own["is_builtin"] != false {
		t.Errorf("Expected the own assessment not flagged builtin, got %v", own["is_builtin"])
	}
	if own["unit"] != "repetitions" {
		t.Errorf("Expected unit repetitions, got %v", own["unit"])
	}
}

// A coach cannot declare somebody else's training an assessment: the assessment
// is the training, so only its owner may say what it measures.
func TestAssessmentDefinitions_RejectsForeignTraining(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, ownerToken := testutil.CreateTestUser(t, queries, "assessdefs2@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "assessdefs3@test.com")
	app := assessmentApp(t, pool, queries)

	status, training := postJSON(t, app, "/api/trainings", ownerToken, map[string]interface{}{
		"title": "Someone else's training",
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d", status)
	}

	status, _ = postJSON(t, app, "/api/assessment-definitions", otherToken, map[string]interface{}{
		"training_id": training["id"],
		"label":       "Stolen",
		"prompt":      "How many?",
		"unit":        "repetitions",
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d declaring a foreign training an assessment, got %d", fiber.StatusBadRequest, status)
	}
}

func TestAssessmentDefinitions_RejectsInvalidUnit(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "assessdefs4@test.com")
	app := assessmentApp(t, pool, queries)

	status, training := postJSON(t, app, "/api/trainings", token, map[string]interface{}{"title": "Test"})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d", status)
	}

	status, _ = postJSON(t, app, "/api/assessment-definitions", token, map[string]interface{}{
		"training_id": training["id"],
		"label":       "Nonsense",
		"prompt":      "How much?",
		"unit":        "furlongs",
	})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected %d for an unknown unit, got %d", fiber.StatusBadRequest, status)
	}
}

// Another user's assessment is not theirs to edit, and the shared ownership
// helper is what has to say so.
func TestAssessmentDefinitions_IsolatesUsers(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, ownerToken := testutil.CreateTestUser(t, queries, "assessdefs5@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "assessdefs6@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, ownerToken, "Private test", "seconds", false)

	body, _ := json.Marshal(map[string]interface{}{
		"label": "Renamed", "prompt": "How long?", "unit": "seconds",
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/assessment-definitions/"+assessmentID, body, otherToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d updating another user's assessment, got %d", fiber.StatusForbidden, resp.StatusCode)
	}

	resp, err = app.Test(testutil.NewJSONRequestWithAuth(http.MethodDelete, "/api/assessment-definitions/"+assessmentID, nil, otherToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d deleting another user's assessment, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}
