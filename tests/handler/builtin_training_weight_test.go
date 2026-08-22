package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

func setupBuiltinWeightApp(t *testing.T, email string) (*fiber.App, string, func()) {
	t.Helper()

	pool, queries := testutil.SetupTestDB(t)
	_, token := testutil.CreateTestUser(t, queries, email)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		BuiltinTrainingWeightHandler: handler.NewBuiltinTrainingWeightHandler(queries),
	})

	return app, token, func() { testutil.CleanupTestDB(t, pool) }
}

func createBuiltinWeight(t *testing.T, app *fiber.App, token, builtinTrainingID string, left, right float64) map[string]interface{} {
	t.Helper()

	body, _ := json.Marshal(map[string]interface{}{
		"id":                  uuid.NewString(),
		"builtin_training_id": builtinTrainingID,
		"custom_weight_left":  left,
		"custom_weight_right": right,
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/builtin-training-weights", body, token)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	return created
}

func TestBuiltinTrainingWeightHandler_Create_ReturnsSnakeCase(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	app, token, cleanup := setupBuiltinWeightApp(t, "weight1@test.com")
	defer cleanup()

	builtinTrainingID := uuid.NewString()
	created := createBuiltinWeight(t, app, token, builtinTrainingID, 12.5, 14.5)

	for _, key := range []string{"id", "user_id", "builtin_training_id", "custom_weight_left", "custom_weight_right", "updated_at"} {
		if _, ok := created[key]; !ok {
			t.Errorf("Expected key %q in response, got %v", key, created)
		}
	}
	if created["builtin_training_id"] != builtinTrainingID {
		t.Errorf("Expected builtin_training_id %q, got %v", builtinTrainingID, created["builtin_training_id"])
	}
	if created["custom_weight_left"] != 12.5 {
		t.Errorf("Expected custom_weight_left 12.5, got %v", created["custom_weight_left"])
	}
	if created["custom_weight_right"] != 14.5 {
		t.Errorf("Expected custom_weight_right 14.5, got %v", created["custom_weight_right"])
	}
	if _, ok := created["BuiltinTrainingID"]; ok {
		t.Errorf("Response still carries Go field names: %v", created)
	}
}

func TestBuiltinTrainingWeightHandler_Create_MissingFields(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	app, token, cleanup := setupBuiltinWeightApp(t, "weight2@test.com")
	defer cleanup()

	body, _ := json.Marshal(map[string]interface{}{"custom_weight_left": 10.0})
	req := testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/builtin-training-weights", body, token)

	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

func TestBuiltinTrainingWeightHandler_Create_DuplicateRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	app, token, cleanup := setupBuiltinWeightApp(t, "weight3@test.com")
	defer cleanup()

	builtinTrainingID := uuid.NewString()
	createBuiltinWeight(t, app, token, builtinTrainingID, 10.0, 11.0)

	body, _ := json.Marshal(map[string]interface{}{
		"id":                  uuid.NewString(),
		"builtin_training_id": builtinTrainingID,
		"custom_weight_left":  20.0,
		"custom_weight_right": 21.0,
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/builtin-training-weights", body, token)

	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("Expected %d for a second override on the same training, got %d", fiber.StatusConflict, resp.StatusCode)
	}
}

func TestBuiltinTrainingWeightHandler_List(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	app, token, cleanup := setupBuiltinWeightApp(t, "weight4@test.com")
	defer cleanup()

	builtinTrainingID := uuid.NewString()
	createBuiltinWeight(t, app, token, builtinTrainingID, 10.0, 11.0)

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/builtin-training-weights", nil, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var weights []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&weights)
	if len(weights) != 1 {
		t.Fatalf("Expected 1 weight override, got %d", len(weights))
	}
	if weights[0]["builtin_training_id"] != builtinTrainingID {
		t.Errorf("Expected builtin_training_id %q, got %v", builtinTrainingID, weights[0]["builtin_training_id"])
	}
}

func TestBuiltinTrainingWeightHandler_Update(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	app, token, cleanup := setupBuiltinWeightApp(t, "weight5@test.com")
	defer cleanup()

	created := createBuiltinWeight(t, app, token, uuid.NewString(), 10.0, 11.0)

	body, _ := json.Marshal(map[string]interface{}{
		"custom_weight_left":  30.0,
		"custom_weight_right": 31.0,
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/builtin-training-weights/"+created["id"].(string), body, token)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	var updated map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&updated)
	if updated["custom_weight_left"] != 30.0 {
		t.Errorf("Expected custom_weight_left 30.0, got %v", updated["custom_weight_left"])
	}
	if updated["custom_weight_right"] != 31.0 {
		t.Errorf("Expected custom_weight_right 31.0, got %v", updated["custom_weight_right"])
	}
}

func TestBuiltinTrainingWeightHandler_Update_OtherUserForbidden(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, ownerToken := testutil.CreateTestUser(t, queries, "weight6owner@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "weight6other@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		BuiltinTrainingWeightHandler: handler.NewBuiltinTrainingWeightHandler(queries),
	})

	created := createBuiltinWeight(t, app, ownerToken, uuid.NewString(), 10.0, 11.0)

	body, _ := json.Marshal(map[string]interface{}{
		"custom_weight_left":  30.0,
		"custom_weight_right": 31.0,
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/builtin-training-weights/"+created["id"].(string), body, otherToken)

	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}

func TestBuiltinTrainingWeightHandler_Delete_OtherUserForbidden(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, ownerToken := testutil.CreateTestUser(t, queries, "weight7owner@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "weight7other@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		BuiltinTrainingWeightHandler: handler.NewBuiltinTrainingWeightHandler(queries),
	})

	created := createBuiltinWeight(t, app, ownerToken, uuid.NewString(), 10.0, 11.0)

	req := testutil.NewRequestWithAuth(http.MethodDelete, "/api/builtin-training-weights/"+created["id"].(string), nil, otherToken)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected %d, got %d", fiber.StatusForbidden, resp.StatusCode)
	}
}
