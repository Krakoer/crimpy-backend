package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	demoVideoLink        = "https://example.com/videos/pull-up"
	demoVideoDescription = "Start from a dead hang, chin over the bar."
)

// setupExerciseVideoApp wires the handlers a video link has to travel through:
// the coach authors it on an exercise, the training item points at that
// exercise, and the athlete reads it back through a program and a played
// session.
func setupExerciseVideoApp(t *testing.T, prefix string) (app *fiber.App, pool *pgxpool.Pool, coachToken, userToken, userID, programID string) {
	t.Helper()
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, prefix+"coach@test.com")
	userID, userToken = testutil.CreateTestUser(t, queries, prefix+"user@test.com")
	enrollUserDirect(t, pool, coachID, userID)

	app = testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
		ProgramHandler:  handler.NewProgramHandler(queries, pool),
		SessionHandler:  handler.NewSessionHandler(queries, pool),
	})
	programID = createTestProgram(t, coachToken, userID, app)
	return app, pool, coachToken, userToken, userID, programID
}

func createExerciseWithVideo(t *testing.T, app *fiber.App, coachToken, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"name":        name,
		"description": demoVideoDescription,
		"video_link":  demoVideoLink,
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/coach/exercises", body, coachToken))
	if err != nil {
		t.Fatalf("Failed to create exercise: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating exercise, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result["id"].(string)
}

// createTrainingUsingExercise builds a training whose single item points at the
// exercise, which is the only way an item carries a video.
func createTrainingUsingExercise(t *testing.T, app *fiber.App, coachToken, exerciseID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"title":         "Pull up session",
		"training_type": "workout",
		"items": []map[string]interface{}{
			{
				"type":        "exercise",
				"position":    0,
				"reps":        8,
				"exercise_id": exerciseID,
			},
		},
	})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/trainings", body, coachToken))
	if err != nil {
		t.Fatalf("Failed to create training: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating training, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result["id"].(string)
}

func assertCarriesVideo(t *testing.T, item map[string]interface{}, where string) {
	t.Helper()
	if item["exercise_video_link"] != demoVideoLink {
		t.Errorf("Expected the video link on %s, got %v", where, item["exercise_video_link"])
	}
	if item["exercise_description"] != demoVideoDescription {
		t.Errorf("Expected the exercise description on %s, got %v", where, item["exercise_description"])
	}
}

func TestTrainingHandler_CoachTrainingCarriesExerciseVideo(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, coachToken, _, _, _ := setupExerciseVideoApp(t, "exvid1")

	exerciseID := createExerciseWithVideo(t, app, coachToken, "Pull up")
	trainingID := createTrainingUsingExercise(t, app, coachToken, exerciseID)

	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, "/api/trainings/"+trainingID, nil, coachToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	assertCarriesVideo(t, firstItem(t, result), "the coach's own training")
}

// The athlete path the ticket is about: the video has to arrive with the
// training, because the exercise itself is out of their reach.
func TestProgramHandler_MyProgramTrainingCarriesExerciseVideo(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, coachToken, userToken, userID, programID := setupExerciseVideoApp(t, "exvid2")

	exerciseID := createExerciseWithVideo(t, app, coachToken, "Pull up")
	trainingID := createTrainingUsingExercise(t, app, coachToken, exerciseID)

	weekBody, _ := json.Marshal(map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
		},
	})
	weekReq := testutil.NewJSONRequestWithAuth(http.MethodPut, fmt.Sprintf("/api/coach/clients/%s/programs/%s/weeks/1", userID, programID), weekBody, coachToken)
	if weekResp, err := app.Test(weekReq); err != nil || weekResp.StatusCode != fiber.StatusOK {
		t.Fatalf("Failed to upsert week: err=%v status=%v", err, weekResp.StatusCode)
	}

	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, fmt.Sprintf("/api/user/programs/%s/trainings/%s", programID, trainingID), nil, userToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	assertCarriesVideo(t, firstItem(t, result), "the athlete's program training")
}

// Without this the link shows on the schedule screen and vanishes during the
// run, since the run reads the frozen prescription rather than the training.
func TestSessionHandler_PrescriptionSnapshotCarriesExerciseVideo(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, coachToken, userToken, userID, programID := setupExerciseVideoApp(t, "exvid3")

	exerciseID := createExerciseWithVideo(t, app, coachToken, "Pull up")
	trainingID := createTrainingUsingExercise(t, app, coachToken, exerciseID)

	created := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{
			{"training_id": trainingID, "day_of_week": 0},
		},
	})
	programSessionID := weekSessionIDs(created)[0]

	session := playSession(t, app, userToken, map[string]interface{}{
		"training_id":        trainingID,
		"program_session_id": programSessionID,
	})

	prescription := sessionPrescription(t, session)
	items := prescriptionItems(t, prescription)
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	assertCarriesVideo(t, items[0].(map[string]interface{}), "the prescription snapshot")
}

// The fact that makes this a backend change rather than an extra fetch from the
// app: the athlete cannot read the exercise the link lives on.
func TestExerciseHandler_AthleteCannotReadExercise(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, coachToken, userToken, _, _ := setupExerciseVideoApp(t, "exvid4")

	exerciseID := createExerciseWithVideo(t, app, coachToken, "Pull up")

	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, "/api/coach/exercises/"+exerciseID, nil, userToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Expected 403 for an athlete reading a coach exercise, got %d", resp.StatusCode)
	}
}

// An item with no exercise behind it must not grow empty strings for the two
// fields: the app tells "no video" from an absent key.
func TestTrainingHandler_ItemWithoutExerciseHasNoVideoFields(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, _, coachToken, _, _, _ := setupExerciseVideoApp(t, "exvid5")

	trainingID, _ := createTestCoachTrainingWithItems(t, coachToken, app)

	resp, err := app.Test(testutil.NewRequestWithAuth(http.MethodGet, "/api/trainings/"+trainingID, nil, coachToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	item := firstItem(t, result)
	if _, present := item["exercise_video_link"]; present {
		t.Errorf("Expected no video link key on an item with no exercise, got %v", item["exercise_video_link"])
	}
	if _, present := item["exercise_description"]; present {
		t.Errorf("Expected no exercise description key on an item with no exercise, got %v", item["exercise_description"])
	}
}
