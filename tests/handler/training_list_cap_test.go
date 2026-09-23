package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"crimpy/backend/internal/db"
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

// listTrainingsWithHeaders reads the library and hands back the truncation
// header next to the rows. The header is the whole contract this file tests, so
// the helper in training_list_include_items_test.go, which drops the response,
// cannot be reused.
func listTrainingsWithHeaders(t *testing.T, app *fiber.App, token, query string) (int, []map[string]interface{}, string) {
	t.Helper()
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/trainings"+query, nil, token), fiber.TestConfig{Timeout: 0})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var list []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&list)
	return resp.StatusCode, list, resp.Header.Get(handler.TrainingsTruncatedHeader)
}

// seedLibrary writes count trainings straight through the queries rather than
// the API. Two hundred and one round trips through the handler would dominate
// the runtime of this file and none of them is what is under test: the cap
// reads a library, it does not care how the rows got there.
//
// Titles are zero padded so their lexicographic order is their numeric one,
// which is what lets the truncated answer be checked for being the first N by
// title rather than merely N rows.
func seedLibrary(t *testing.T, queries *db.Queries, userID string, count int) {
	t.Helper()
	var owner pgtype.UUID
	if err := owner.Scan(userID); err != nil {
		t.Fatalf("Failed to read the user id: %v", err)
	}
	for i := 0; i < count; i++ {
		if _, err := queries.CreateTraining(context.Background(), db.CreateTrainingParams{
			UserID:       owner,
			Title:        fmt.Sprintf("Cap %04d", i),
			TrainingType: "climbing",
		}); err != nil {
			t.Fatalf("Failed to seed training %d: %v", i, err)
		}
	}
}

// A library sitting exactly on the ceiling is not a truncated one. Asserting
// the header's absence here is the point: a cap written with the wrong
// comparison answers every row and still claims it cut the library, and a
// client that believes it shows the coach a warning about nothing.
func TestTrainings_IncludeItemsDoesNotFlagALibraryAtTheCap(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	userID, token := testutil.CreateTestUser(t, queries, "capatlimit@test.com")
	app := assessmentApp(t, pool, queries)

	seedLibrary(t, queries, userID, handler.MaxTrainingsWithItems)

	status, list, truncated := listTrainingsWithHeaders(t, app, token, "?include=items")
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 listing the library, got %d", status)
	}
	if len(list) != handler.MaxTrainingsWithItems {
		t.Errorf("Expected the whole library of %d rows, got %d", handler.MaxTrainingsWithItems, len(list))
	}
	if truncated != "" {
		t.Errorf("Expected no truncation header on a library at the cap, got %q", truncated)
	}
}

// One row past the ceiling is where the header has to appear, and the answer
// has to be the first MaxTrainingsWithItems rows by title rather than an
// arbitrary subset: GetTrainings orders by title, so the same library cut twice
// answers the same rows, and a client that reads a cut library twice does not
// see it change under it.
func TestTrainings_IncludeItemsCutsAndFlagsALibraryOverTheCap(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	userID, token := testutil.CreateTestUser(t, queries, "capovercap@test.com")
	app := assessmentApp(t, pool, queries)

	seedLibrary(t, queries, userID, handler.MaxTrainingsWithItems+1)

	status, list, truncated := listTrainingsWithHeaders(t, app, token, "?include=items")
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 listing the library, got %d", status)
	}
	if len(list) != handler.MaxTrainingsWithItems {
		t.Fatalf("Expected the answer cut to %d rows, got %d", handler.MaxTrainingsWithItems, len(list))
	}
	if truncated != "true" {
		t.Errorf("Expected the truncation header on a cut library, got %q", truncated)
	}
	if got := list[0]["title"]; got != "Cap 0000" {
		t.Errorf("Expected the cut to keep the first row by title, got %v", got)
	}
	want := fmt.Sprintf("Cap %04d", handler.MaxTrainingsWithItems-1)
	if got := list[len(list)-1]["title"]; got != want {
		t.Errorf("Expected the cut to end at %q, got %v", want, got)
	}
	// The rows it does answer are still whole ones. A cap that cut the list
	// after the items were attached would pass every assertion above while
	// paying the cost the ceiling exists to avoid, so the rows are checked for
	// carrying the key that says the tree was read.
	if _, ok := list[0]["items"]; !ok {
		t.Errorf("Expected the surviving rows to carry their items, got %v", keysOf(list[0]))
	}
}

// The cheap list is deliberately not capped. It carries no item trees, which is
// the cost the ceiling is about, and a client reading it expects its whole
// library the way it always has.
func TestTrainings_CheapListIsNotCapped(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	userID, token := testutil.CreateTestUser(t, queries, "capcheap@test.com")
	app := assessmentApp(t, pool, queries)

	seedLibrary(t, queries, userID, handler.MaxTrainingsWithItems+1)

	status, list, truncated := listTrainingsWithHeaders(t, app, token, "")
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 listing the library, got %d", status)
	}
	if len(list) != handler.MaxTrainingsWithItems+1 {
		t.Errorf("Expected the whole library of %d rows, got %d", handler.MaxTrainingsWithItems+1, len(list))
	}
	if truncated != "" {
		t.Errorf("Expected no truncation header on an uncapped cheap list, got %q", truncated)
	}
}

// The ceiling is counted per caller, not across the table. A coach whose own
// library is small must not be handed a truncation header because somebody
// else's is large, which is what a cap applied before the ownership filter
// would do.
func TestTrainings_CapCountsOnlyTheCallersOwnLibrary(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	crowdedID, _ := testutil.CreateTestUser(t, queries, "capcrowded@test.com")
	_, quietToken := testutil.CreateTestUser(t, queries, "capquiet@test.com")
	app := assessmentApp(t, pool, queries)

	seedLibrary(t, queries, crowdedID, handler.MaxTrainingsWithItems+1)

	status, list, truncated := listTrainingsWithHeaders(t, app, quietToken, "?include=items")
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 listing the library, got %d", status)
	}
	if len(list) != 0 {
		t.Errorf("Expected an empty library, got %d rows", len(list))
	}
	if truncated != "" {
		t.Errorf("Expected no truncation header for a coach with no trainings, got %q", truncated)
	}
}
