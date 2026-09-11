package handler_test

import (
	"context"
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setupExerciseOwnershipApp gives two coaches who share nothing, which is what
// an unowned reference needs to be written from.
func setupExerciseOwnershipApp(t *testing.T, prefix string) (app *fiber.App, pool *pgxpool.Pool, ownerToken, otherToken string) {
	t.Helper()
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	_, ownerToken = testutil.CreateTestValidatedCoachUser(t, pool, queries, prefix+"owner@test.com")
	_, otherToken = testutil.CreateTestValidatedCoachUser(t, pool, queries, prefix+"other@test.com")

	app = testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})
	return app, pool, ownerToken, otherToken
}

func postTrainingWithExercise(t *testing.T, app *fiber.App, token, exerciseID string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"title":         "Pull up session",
		"training_type": "workout",
		"items": []map[string]interface{}{
			{"type": "exercise", "position": 0, "reps": 8, "exercise_id": exerciseID},
		},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/trainings", body, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	return resp
}

// The hole this is about: without a write-time check, any authenticated user
// could store a reference to another coach's exercise and read its name, notes
// and video back off a training of their own.
func TestTrainingHandler_RefusesAnotherCoachsExercise(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, ownerToken, otherToken := setupExerciseOwnershipApp(t, "exown1")

	exerciseID := createExerciseWithVideo(t, app, ownerToken, "Weighted pull up")

	resp := postTrainingWithExercise(t, app, otherToken, exerciseID)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for another coach's exercise, got %d", resp.StatusCode)
	}
}

func TestTrainingHandler_AcceptsOwnExercise(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, ownerToken, _ := setupExerciseOwnershipApp(t, "exown2")

	exerciseID := createExerciseWithVideo(t, app, ownerToken, "Weighted pull up")

	resp := postTrainingWithExercise(t, app, ownerToken, exerciseID)
	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected 201 for the coach's own exercise, got %d", resp.StatusCode)
	}
}

// An id that exists nowhere is refused the same way and with the same message as
// one belonging to somebody else, so the answer cannot be used to find out
// whether a guessed id is real.
func TestTrainingHandler_RefusesUnknownExerciseIdentically(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, ownerToken, otherToken := setupExerciseOwnershipApp(t, "exown3")

	exerciseID := createExerciseWithVideo(t, app, ownerToken, "Weighted pull up")

	unowned := postTrainingWithExercise(t, app, otherToken, exerciseID)
	missing := postTrainingWithExercise(t, app, otherToken, "5c1e0f5e-0000-4000-8000-000000000000")

	if unowned.StatusCode != missing.StatusCode {
		t.Fatalf("Expected the same status, got %d and %d", unowned.StatusCode, missing.StatusCode)
	}
	var a, b map[string]interface{}
	json.NewDecoder(unowned.Body).Decode(&a)
	json.NewDecoder(missing.Body).Decode(&b)
	if a["error"] != b["error"] {
		t.Errorf("Expected the same message, got %q and %q", a["error"], b["error"])
	}
}

func TestTrainingHandler_RefusesAMalformedExerciseID(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, ownerToken, _ := setupExerciseOwnershipApp(t, "exown4")

	resp := postTrainingWithExercise(t, app, ownerToken, "not-a-uuid")
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for a malformed exercise id, got %d", resp.StatusCode)
	}
}

// An empty string means "no exercise", which is what every item that is not an
// exercise carries. It must not be read as a malformed id.
func TestTrainingHandler_TakesAnEmptyExerciseID(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, ownerToken, _ := setupExerciseOwnershipApp(t, "exown5")

	resp := postTrainingWithExercise(t, app, ownerToken, "")
	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Expected 201 for an empty exercise id, got %d", resp.StatusCode)
	}
}

func TestTrainingHandler_UpdateRefusesAnotherCoachsExercise(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, ownerToken, otherToken := setupExerciseOwnershipApp(t, "exown6")

	exerciseID := createExerciseWithVideo(t, app, ownerToken, "Weighted pull up")

	created := postTrainingWithExercise(t, app, otherToken, "")
	if created.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d", created.StatusCode)
	}
	var training map[string]interface{}
	json.NewDecoder(created.Body).Decode(&training)

	body, _ := json.Marshal(map[string]interface{}{
		"title":         "Pull up session",
		"training_type": "workout",
		"items": []map[string]interface{}{
			{"type": "exercise", "position": 0, "reps": 8, "exercise_id": exerciseID},
		},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/trainings/"+training["id"].(string), body, otherToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 updating onto another coach's exercise, got %d", resp.StatusCode)
	}
}

// The other half of the fix, and the half that covers rows stored before the
// write check existed: the read join is closed on the training's owner, so an
// unowned reference already in the database resolves to no exercise rather than
// handing over somebody else's name, notes and video.
func TestTrainingHandler_StoredUnownedExerciseDisclosesNothing(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, pool, ownerToken, otherToken := setupExerciseOwnershipApp(t, "exown7")

	exerciseID := createExerciseWithVideo(t, app, ownerToken, "Weighted pull up")

	created := postTrainingWithExercise(t, app, otherToken, "")
	if created.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d", created.StatusCode)
	}
	var training map[string]interface{}
	json.NewDecoder(created.Body).Decode(&training)
	trainingID := training["id"].(string)

	// Written straight to the database, which is the only way to produce the
	// row the write check now refuses.
	if _, err := pool.Exec(context.Background(),
		"UPDATE training_items SET exercise_id = $1 WHERE training_id = $2",
		exerciseID, trainingID); err != nil {
		t.Fatalf("Failed to plant the unowned reference: %v", err)
	}

	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, "/api/trainings/"+trainingID, nil, otherToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	item := firstItem(t, result)
	for _, key := range []string{"exercise_name", "exercise_description", "exercise_comment", "exercise_video_link"} {
		if value, present := item[key]; present {
			t.Errorf("Expected no %s for an unowned exercise, got %v", key, value)
		}
	}
	// The item itself still reads, so the training is not broken by the
	// reference it can no longer resolve.
	if item["reps"] != float64(8) {
		t.Errorf("Expected the item to still carry its reps, got %v", item["reps"])
	}
}

// Nesting is the obvious next thing to try, and it is the shape a real training
// uses. Without the recursion in requestExerciseIDs the reference would be
// stored and read back, which is the same disclosure one level down.
func TestTrainingHandler_RefusesAnotherCoachsExerciseInsideACircuit(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, ownerToken, otherToken := setupExerciseOwnershipApp(t, "exown8")

	exerciseID := createExerciseWithVideo(t, app, ownerToken, "Weighted pull up")

	body, _ := json.Marshal(map[string]interface{}{
		"title":         "Pull up session",
		"training_type": "workout",
		"items": []map[string]interface{}{
			{
				"type":     "circuit",
				"position": 0,
				"cycles":   3,
				"items": []map[string]interface{}{
					{"type": "exercise", "position": 0, "reps": 8, "exercise_id": exerciseID},
				},
			},
		},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/trainings", body, otherToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for another coach's exercise nested in a circuit, got %d", resp.StatusCode)
	}
}

// A payload error stays a 400 the client can act on; nothing on this path turns
// a database failure into one, which would tell a coach their training is
// malformed when the database is simply down.
func TestTrainingHandler_MalformedNestedExerciseIDIsAPayloadError(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, ownerToken, _ := setupExerciseOwnershipApp(t, "exown9")

	body, _ := json.Marshal(map[string]interface{}{
		"title":         "Pull up session",
		"training_type": "workout",
		"items": []map[string]interface{}{
			{
				"type":     "group",
				"position": 0,
				"items": []map[string]interface{}{
					{"type": "exercise", "position": 0, "reps": 8, "exercise_id": "not-a-uuid"},
				},
			},
		},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/trainings", body, ownerToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a malformed nested exercise id, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	message, _ := result["error"].(string)
	if !strings.Contains(message, "not-a-uuid") {
		t.Errorf("Expected the message to name the bad id, got %q", message)
	}
}
