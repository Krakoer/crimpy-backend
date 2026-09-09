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

// programReadAppWithFailingQuery serves the program reads through the fault
// seam: sqlc queries over the real pool with one named query failing and every
// other one running for real. Only this app carries the fault, so whatever the
// test set up was written through the healthy one.
//
// The pool is handed over as the pool too, since that is what the handler
// begins its transactions on. A fault therefore reaches the queries a handler
// runs directly, not the ones it runs on a transaction.
func programReadAppWithFailingQuery(t *testing.T, pool *pgxpool.Pool, queryName string) (*fiber.App, *testutil.FaultyDBTX) {
	t.Helper()
	queries, faulty := testutil.FailingQueries(pool, testutil.QueryNamed(queryName))
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		ProgramHandler: handler.NewProgramHandler(queries, pool),
	})
	return app, faulty
}

// getJSON reads a path and returns the status with the decoded body, for the
// reads whose status is itself under test.
func getJSON(t *testing.T, app *fiber.App, path, token string) (int, map[string]interface{}) {
	t.Helper()
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, path, nil, token))
	if err != nil {
		t.Fatalf("Request to %s failed: %v", path, err)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// createTargetingTraining makes a training whose single item reads its rep
// count off an assessment, so the read of it names one of its own for an
// override's to be added to.
func createTargetingTraining(t *testing.T, app *fiber.App, coachToken, title, assessmentID string) (trainingID, itemID string) {
	t.Helper()
	status, training := postJSON(t, app, "/api/trainings", coachToken, map[string]interface{}{
		"title": title,
		"items": []map[string]interface{}{{
			"type": "exercise",
			"reps": 10,
			"variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{
					"assessment_id": assessmentID, "percent": 75, "fallback": 10,
				},
			},
		}},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating %q, got %d: %v", title, status, training)
	}
	item := training["items"].([]interface{})[0].(map[string]interface{})
	return training["id"].(string), item["id"].(string)
}

// The degrade rather than fail path Krakoer/crimpy#92 put in. Naming the
// assessments a week override reads against costs a label and nothing else, so
// a failure there serves the training without those names instead of stopping
// an athlete opening a session they are scheduled to play. Nothing pinned that
// branch before: a refactor letting the error propagate would have kept the
// suite green while handing the athlete a 500 on a core read.
func TestProgramTraining_ServesTheTrainingWhenNamingOverrideAssessmentsFails(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	s := setupAssessmentProgram(t, "overridefault")

	_, itemAssessmentID := createAssessmentTraining(t, s.App, s.CoachToken, "Max pull ups", "repetitions", false)
	_, overrideAssessmentID := createAssessmentTraining(t, s.App, s.CoachToken, "Max dips", "repetitions", false)

	trainingID, itemID := createTargetingTraining(t, s.App, s.CoachToken, "Pull day", itemAssessmentID)
	scheduleWithOverride(t, s.App, s.CoachToken, s.UserID, s.ProgramID, trainingID, itemID, 1, map[string]interface{}{
		"variable_targets": map[string]interface{}{
			"reps": map[string]interface{}{
				"assessment_id": overrideAssessmentID, "percent": 50, "fallback": 8,
			},
		},
	})

	healthy := namedAssessments(myProgramTraining(t, s.App, s.UserToken, s.ProgramID, trainingID))
	if _, ok := healthy[overrideAssessmentID]; !ok {
		t.Fatalf("Expected the healthy read to name the override's assessment, got %v", healthy)
	}

	app, faulty := programReadAppWithFailingQuery(t, s.Pool, "GetMyProgramTrainingOverrides")
	url := fmt.Sprintf("/api/user/programs/%s/trainings/%s", s.ProgramID, trainingID)
	status, training := getJSON(t, app, url, s.UserToken)

	if faulty.Injected() != 1 {
		t.Fatalf("Expected the fault to land once on the read, got %d", faulty.Injected())
	}
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 with the overrides query failing, got %d: %v", status, training)
	}
	named := namedAssessments(training)
	if _, ok := named[itemAssessmentID]; !ok {
		t.Errorf("Expected the training's own referenced assessment still named, got %v", training["referenced_assessments"])
	}
	if _, ok := named[overrideAssessmentID]; ok {
		t.Errorf("Expected the override's assessment left unnamed once its query failed, got %v", training["referenced_assessments"])
	}
	if len(named) != 1 {
		t.Errorf("Expected referenced_assessments to be the training's own list alone, got %v", training["referenced_assessments"])
	}
	items, ok := training["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Errorf("Expected the training's item served all the same, got %v", training["items"])
	}
}

// The other half of the same read: dropStaleWeekOverrides hides an override the
// training item no longer takes rather than refusing the week, but a query
// failure inside that check is not degraded. The read answers 500 rather than
// serving overrides it could not revalidate, which would promise the athlete a
// shape the session started from them refuses to freeze. Pinned as the failure
// it is, so the day it starts degrading is a deliberate change and not a silent
// one.
func TestMyWeek_FailsWhenItCannotRevalidateItsOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	s := setupAssessmentProgram(t, "weekrevalfault")

	_, assessmentID := createAssessmentTraining(t, s.App, s.CoachToken, "Max pull ups", "repetitions", false)
	trainingID, itemID := createTargetingTraining(t, s.App, s.CoachToken, "Pull day", assessmentID)
	scheduleWithOverride(t, s.App, s.CoachToken, s.UserID, s.ProgramID, trainingID, itemID, 1, map[string]interface{}{
		"variable_targets": map[string]interface{}{
			"reps": map[string]interface{}{
				"assessment_id": assessmentID, "percent": 50, "fallback": 8,
			},
		},
	})

	url := fmt.Sprintf("/api/user/programs/%s/weeks/1", s.ProgramID)
	if mine := weekOverrides(t, s.App, url, s.UserToken); len(mine) != 1 {
		t.Fatalf("Expected the healthy read to carry the one override, got %v", mine)
	}

	app, faulty := programReadAppWithFailingQuery(t, s.Pool, "GetAssessmentDefinitionsByIDs")
	status, week := getJSON(t, app, url, s.UserToken)

	if faulty.Injected() != 1 {
		t.Fatalf("Expected the fault to land once on the read, got %d", faulty.Injected())
	}
	if status != fiber.StatusInternalServerError {
		t.Fatalf("Expected 500 with the revalidation query failing, got %d: %v", status, week)
	}
	if _, served := week["sessions"]; served {
		t.Errorf("Expected no week served when its overrides could not be revalidated, got %v", week)
	}
}
