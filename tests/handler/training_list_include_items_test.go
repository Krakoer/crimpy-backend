package handler_test

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"crimpy/backend/tests/testutil"

	"github.com/gofiber/fiber/v3"
)

// listTrainings reads the library and hands back the decoded rows next to the
// raw body, since what the cheap list does not say is as much the contract as
// what it does.
func listTrainings(t *testing.T, app *fiber.App, token, query string) (int, []map[string]interface{}) {
	t.Helper()
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/trainings"+query, nil, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var list []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&list)
	return resp.StatusCode, list
}

func rowNamed(t *testing.T, list []map[string]interface{}, title string) map[string]interface{} {
	t.Helper()
	for _, row := range list {
		if row["title"] == title {
			return row
		}
	}
	t.Fatalf("Expected a training named %q in %v", title, list)
	return nil
}

func keysOf(row map[string]interface{}) []string {
	keys := make([]string, 0, len(row))
	for key := range row {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The frontend reads this list and must keep getting exactly what it got before
// include existed, so the fields are spelled out here rather than derived from
// the response type: a field quietly added to the cheap row fails this.
func TestTrainings_ListWithoutIncludeCarriesNoItems(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "listcheap@test.com")
	app := assessmentApp(t, pool, queries)

	if status, body := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Pull ups",
		"items": []map[string]interface{}{{"type": "free", "comment": "three sets"}},
	}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, body)
	}

	status, list := listTrainings(t, app, token, "")
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 listing the library, got %d", status)
	}
	if len(list) != 1 {
		t.Fatalf("Expected one training, got %v", list)
	}

	want := []string{
		"comment",
		"created_at",
		"description",
		"goal",
		"id",
		"is_favorite",
		"title",
		"training_type",
		"updated_at",
		"user_id",
	}
	if got := keysOf(list[0]); !equalStrings(got, want) {
		t.Errorf("Expected the cheap row to carry exactly %v, got %v", want, got)
	}
}

// An assessment training marks the row with its definition, which is the one
// extra key the cheap list has always carried.
func TestTrainings_CheapListStillMarksAnAssessment(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "listassess@test.com")
	app := assessmentApp(t, pool, queries)

	createAssessmentTraining(t, app, token, "Max pull ups", "repetitions", false)

	_, list := listTrainings(t, app, token, "")
	row := rowNamed(t, list, "Max pull ups")
	if _, ok := row["items"]; ok {
		t.Errorf("Expected no items on the cheap row, got %v", row["items"])
	}
	definition, ok := row["assessment"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected the assessment definition on the row, got %v", row["assessment"])
	}
	if definition["unit"] != "repetitions" {
		t.Errorf("Expected the unit on the snapshot, got %v", definition["unit"])
	}

	// The cheap snapshot is the shallow one it has always been, down to the
	// fields it leaves off. Spelled out for the same reason the row above is.
	want := []string{"bodyweight_relative", "id", "label", "per_hand", "prompt", "unit", "unit_locked"}
	if got := keysOf(definition); !equalStrings(got, want) {
		t.Errorf("Expected the cheap snapshot to carry exactly %v, got %v", want, got)
	}
}

// A client reading the list instead of the detail endpoint has to get the same
// snapshot, because an assessment with no training_id on it reads as one Crimpy
// ships rather than as the athlete's own.
func TestTrainings_ListWithIncludeItemsDeepensTheAssessmentSnapshot(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "listsnapshot@test.com")
	app := assessmentApp(t, pool, queries)

	trainingID, assessmentID := createAssessmentTraining(t, app, token, "Max pull ups", "repetitions", false)

	_, list := listTrainings(t, app, token, "?include=items")
	row := rowNamed(t, list, "Max pull ups")
	definition, ok := row["assessment"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected the assessment definition on the row, got %v", row["assessment"])
	}
	if definition["id"] != assessmentID {
		t.Errorf("Expected the definition id, got %v", definition["id"])
	}
	if definition["training_id"] != trainingID {
		t.Errorf("Expected the training the assessment is run from, got %v", definition["training_id"])
	}
	if definition["unit_locked"] != false {
		t.Errorf("Expected a fresh assessment to be free to re-unit, got %v", definition["unit_locked"])
	}

	// A training reading a number against it locks the unit, and the row has to
	// say so exactly as the detail endpoint does.
	if status, body := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Volume day",
		"items": []map[string]interface{}{
			{
				"type": "exercise",
				"reps": 8,
				"variable_targets": map[string]interface{}{
					"reps": map[string]interface{}{
						"assessment_id": assessmentID, "percent": 60, "fallback": 8,
					},
				},
			},
		},
	}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training that reads it, got %d: %v", status, body)
	}

	_, list = listTrainings(t, app, token, "?include=items")
	definition = rowNamed(t, list, "Max pull ups")["assessment"].(map[string]interface{})
	if definition["unit_locked"] != true {
		t.Errorf("Expected the unit to be locked once a training reads it, got %v", definition["unit_locked"])
	}

	// And the cheap list still answers the shallow snapshot, unchanged.
	_, cheap := listTrainings(t, app, token, "")
	cheapDefinition := rowNamed(t, cheap, "Max pull ups")["assessment"].(map[string]interface{})
	if _, ok := cheapDefinition["training_id"]; ok {
		t.Errorf("Expected no training_id on the cheap snapshot, got %v", cheapDefinition["training_id"])
	}
}

// The whole point of the ticket: one request answers with the library and its
// items, so the app never follows the list with a read per training.
func TestTrainings_ListWithIncludeItemsCarriesTheTree(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "listitems@test.com")
	app := assessmentApp(t, pool, queries)

	if status, body := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Repeaters",
		"items": []map[string]interface{}{
			{
				"type":        "group",
				"group_title": "Warm up",
				"items": []map[string]interface{}{
					{"type": "free", "comment": "easy traverses"},
					{"type": "free", "comment": "mobility"},
				},
			},
			{"type": "free", "comment": "hangs"},
		},
	}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, body)
	}
	if status, body := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Empty day",
	}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the empty training, got %d: %v", status, body)
	}

	status, list := listTrainings(t, app, token, "?include=items")
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 listing with the items, got %d", status)
	}

	repeaters := rowNamed(t, list, "Repeaters")
	items, ok := repeaters["items"].([]interface{})
	if !ok {
		t.Fatalf("Expected the item tree on the row, got %v", repeaters["items"])
	}
	if len(items) != 2 {
		t.Fatalf("Expected two top level items, got %v", items)
	}

	group := items[0].(map[string]interface{})
	if group["group_title"] != "Warm up" {
		t.Errorf("Expected the group first, got %v", group)
	}
	children, ok := group["items"].([]interface{})
	if !ok || len(children) != 2 {
		t.Fatalf("Expected the group to carry its two children, got %v", group["items"])
	}
	if children[0].(map[string]interface{})["comment"] != "easy traverses" {
		t.Errorf("Expected the children in the order they were written, got %v", children)
	}
	if children[1].(map[string]interface{})["comment"] != "mobility" {
		t.Errorf("Expected the children in the order they were written, got %v", children)
	}
	if items[1].(map[string]interface{})["comment"] != "hangs" {
		t.Errorf("Expected the second top level item, got %v", items[1])
	}

	// A training with nothing in it answers with an empty array rather than
	// nothing at all, so a caller that asked for items can tell it apart from a
	// row that was never asked to carry any.
	empty := rowNamed(t, list, "Empty day")
	emptyItems, ok := empty["items"].([]interface{})
	if !ok {
		t.Fatalf("Expected an items array on a training with no items, got %v", empty["items"])
	}
	if len(emptyItems) != 0 {
		t.Errorf("Expected no items on the empty training, got %v", emptyItems)
	}
}

// The app resolves a percentage against the definitions the training names, so
// include=items has to hand them over the way the detail endpoint does.
func TestTrainings_ListWithIncludeItemsCarriesReferencedAssessments(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "listrefs@test.com")
	app := assessmentApp(t, pool, queries)

	_, assessmentID := createAssessmentTraining(t, app, token, "Max pull ups", "repetitions", false)

	if status, body := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Volume day",
		"items": []map[string]interface{}{
			{
				"type": "exercise",
				"reps": 8,
				"variable_targets": map[string]interface{}{
					"reps": map[string]interface{}{
						"assessment_id": assessmentID, "percent": 60, "fallback": 8,
					},
				},
			},
		},
	}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, body)
	}

	_, list := listTrainings(t, app, token, "?include=items")

	volume := rowNamed(t, list, "Volume day")
	referenced, ok := volume["referenced_assessments"].([]interface{})
	if !ok || len(referenced) != 1 {
		t.Fatalf("Expected the referenced definition on the row, got %v", volume["referenced_assessments"])
	}
	definition := referenced[0].(map[string]interface{})
	if definition["id"] != assessmentID {
		t.Errorf("Expected the definition the item names, got %v", definition["id"])
	}
	if definition["unit"] != "repetitions" {
		t.Errorf("Expected the unit on the referenced definition, got %v", definition["unit"])
	}

	// A training that references nothing carries no definitions, the same way
	// the detail endpoint leaves the key out.
	maxPullUps := rowNamed(t, list, "Max pull ups")
	if _, ok := maxPullUps["referenced_assessments"]; ok {
		t.Errorf("Expected no referenced assessments on a training that names none, got %v",
			maxPullUps["referenced_assessments"])
	}
}

// One query reads the items of the whole library, so the rows have to be split
// back by training rather than pooled.
func TestTrainings_ListWithIncludeItemsKeepsEachTrainingsItems(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "listsplit@test.com")
	app := assessmentApp(t, pool, queries)

	for _, training := range []struct {
		title    string
		comments []string
	}{
		{"Alpha", []string{"a one", "a two", "a three"}},
		{"Bravo", []string{"b one"}},
		{"Charlie", []string{"c one", "c two"}},
	} {
		items := make([]map[string]interface{}, 0, len(training.comments))
		for _, comment := range training.comments {
			items = append(items, map[string]interface{}{"type": "free", "comment": comment})
		}
		if status, body := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
			"title": training.title,
			"items": items,
		}); status != fiber.StatusCreated {
			t.Fatalf("Expected 201 creating %s, got %d: %v", training.title, status, body)
		}
	}

	_, list := listTrainings(t, app, token, "?include=items")

	for title, want := range map[string][]string{
		"Alpha":   {"a one", "a two", "a three"},
		"Bravo":   {"b one"},
		"Charlie": {"c one", "c two"},
	} {
		row := rowNamed(t, list, title)
		items, ok := row["items"].([]interface{})
		if !ok {
			t.Fatalf("Expected items on %s, got %v", title, row["items"])
		}
		got := make([]string, 0, len(items))
		for _, item := range items {
			got = append(got, item.(map[string]interface{})["comment"].(string))
		}
		if !equalStrings(got, want) {
			t.Errorf("Expected %s to carry %v, got %v", title, want, got)
		}
	}
}

// The list is scoped to the caller, and asking for the items must not widen it.
func TestTrainings_ListWithIncludeItemsStaysWithinTheCaller(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, ownerToken := testutil.CreateTestUser(t, queries, "listowner@test.com")
	_, otherToken := testutil.CreateTestUser(t, queries, "listother@test.com")
	app := assessmentApp(t, pool, queries)

	if status, body := postJSON(t, app, "/api/trainings", ownerToken, map[string]interface{}{
		"title": "Owner only",
		"items": []map[string]interface{}{{"type": "free", "comment": "private"}},
	}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, body)
	}

	status, list := listTrainings(t, app, otherToken, "?include=items")
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 listing another athlete's library, got %d", status)
	}
	if len(list) != 0 {
		t.Errorf("Expected another athlete to see none of the owner's library, got %v", list)
	}
}

// include narrows the library the same way is_assessment does, and the two have
// to hold together.
func TestTrainings_ListWithIncludeItemsRespectsTheAssessmentFilter(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "listboth@test.com")
	app := assessmentApp(t, pool, queries)

	createAssessmentTraining(t, app, token, "Max pull ups", "repetitions", false)
	if status, body := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
		"title": "Plain training",
		"items": []map[string]interface{}{{"type": "free", "comment": "hangs"}},
	}); status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the plain training, got %d: %v", status, body)
	}

	_, list := listTrainings(t, app, token, "?is_assessment=false&include=items")
	if len(list) != 1 {
		t.Fatalf("Expected only the plain training, got %v", list)
	}
	items, ok := list[0]["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("Expected the filtered row to still carry its items, got %v", list[0]["items"])
	}
}

// A misspelled include is refused rather than ignored: answering with the cheap
// list would hand the caller a library of trainings that all look empty.
func TestTrainings_ListRefusesAnUnknownInclude(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "listbadinclude@test.com")
	app := assessmentApp(t, pool, queries)

	for _, query := range []string{"?include=item", "?include=items,everything", "?include=ITEMS"} {
		status, _ := listTrainings(t, app, token, query)
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected %d for %s, got %d", fiber.StatusBadRequest, query, status)
		}
	}

	// An empty include is the cheap list, not a refusal: a client building the
	// query string can leave the value off without being turned away.
	if status, _ := listTrainings(t, app, token, "?include="); status != fiber.StatusOK {
		t.Errorf("Expected 200 for an empty include, got %d", status)
	}
}
