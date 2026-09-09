package handler_test

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Krakoer/crimpy#100: the coach read flags a stale override, and the flag alone
// is not enough for the editor to keep flagging the block while the coach edits
// around it. The refusal is about one field of the override row, so the read has
// to say which, or a reader reasoning about the row as a whole drops the marking
// on any edit anywhere and then saves into the very refusal it stopped showing.
// These tests pin which field each refusal is attributed to.

// staleFieldReasons reads the attribution off a coach week override entry, as
// the reasons attributed to each field.
func staleFieldReasons(t *testing.T, entry map[string]interface{}) map[string][]string {
	t.Helper()
	attributed, told := entry["stale_fields"].([]interface{})
	if !told {
		t.Fatalf("Expected stale_fields on the flagged override, got %v", entry)
	}
	if len(attributed) == 0 {
		t.Fatalf("Expected stale_fields to attribute the refusal, got %v", entry)
	}
	byField := map[string][]string{}
	for _, one := range attributed {
		refusal, ok := one.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected an attributed refusal object, got %v", one)
		}
		field, named := refusal["field"].(string)
		if !named || field == "" {
			t.Fatalf("Expected the refusal attributed to a field, got %v", refusal)
		}
		reason, given := refusal["reason"].(string)
		if !given || reason == "" {
			t.Fatalf("Expected the refusal to carry its reason beside the field, got %v", refusal)
		}
		byField[field] = append(byField[field], reason)
	}
	return byField
}

// staleFieldNames is every field the refusals of one override are attributed to.
func staleFieldNames(t *testing.T, entry map[string]interface{}) []string {
	t.Helper()
	return slices.Sorted(maps.Keys(staleFieldReasons(t, entry)))
}

// refusalCase is one refusal kind staged on an item of its own: the item the
// week was prescribed against, the override the coach set on it, and the item as
// the coach left it afterwards, which is what makes that override stale.
type refusalCase struct {
	name     string
	base     map[string]interface{}
	override map[string]interface{}
	edited   map[string]interface{}
	fields   []string
}

// stageRefusalCases puts every case on its own item of one training, prescribes
// one week session carrying every override, then edits the training to what each
// case leaves behind. The whole table is answered by a single coach read, since
// a refusal is decided per item and the cases cannot reach each other.
func stageRefusalCases(t *testing.T, app *fiber.App, coachToken, userID, programID string, cases []refusalCase) map[string]map[string]interface{} {
	t.Helper()

	bases := make([]map[string]interface{}, 0, len(cases))
	for _, c := range cases {
		bases = append(bases, c.base)
	}
	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, bases)

	overrides := make([]map[string]interface{}, 0, len(cases))
	for i, c := range cases {
		overrides = append(overrides, map[string]interface{}{"item_id": itemIDs[i], "overrides": c.override})
	}
	prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{"overrides": overrides})

	edited := make([]map[string]interface{}, 0, len(cases))
	for i, c := range cases {
		item := map[string]interface{}{"id": itemIDs[i]}
		for key, value := range c.edited {
			item[key] = value
		}
		edited = append(edited, item)
	}
	editStaleOverrideTraining(t, app, coachToken, trainingID, edited)

	entries := weekOverrideEntries(t, app, weekURL(userID, programID, 1), coachToken)
	byCase := make(map[string]map[string]interface{}, len(cases))
	for i, c := range cases {
		entry, present := entries[itemIDs[i]]
		if !present {
			t.Fatalf("Expected the %s override in the coach week, got %v", c.name, entries)
		}
		byCase[c.name] = entry
	}
	return byCase
}

func kgLoads(count int) []map[string]interface{} {
	loads := make([]map[string]interface{}, 0, count)
	for i := 0; i < count; i++ {
		loads = append(loads, map[string]interface{}{"unit": "kg", "value": 20})
	}
	return loads
}

func grips(count int) []string {
	held := make([]string, 0, count)
	for i := 0; i < count; i++ {
		held = append(held, "HC")
	}
	return held
}

func edges(count int) []int {
	sizes := make([]int, 0, count)
	for i := 0; i < count; i++ {
		sizes = append(sizes, 20)
	}
	return sizes
}

// Every refusal a stale override can carry, attributed to the field it is about.
// The list is the one Krakoer/crimpy#100 enumerates: a named configuration
// array, the AMRAP marker, the emom timing fields, and the fields that describe
// a repeated hang.
func TestWeekHandler_GetWeek_AttributesEachRefusalToItsField(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, userID, programID := setupAssessmentProgramApp(t, "attribfield")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)
	repsTarget := map[string]interface{}{
		"reps": map[string]interface{}{"assessment_id": assessmentID, "percent": 50, "fallback": 10},
	}

	cases := []refusalCase{
		{
			name:     "loads against a shrunk grid",
			base:     map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 3, "loads": kgLoads(3)},
			override: map[string]interface{}{"loads": kgLoads(3)},
			edited:   map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 2, "loads": kgLoads(2)},
			fields:   []string{"loads"},
		},
		{
			// The one the editor cannot answer from the override row alone: the
			// refused array is the item's own, which the override never carried,
			// so a reader has to be told the name rather than look for a key.
			name:     "an array the override does not carry",
			base:     map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 3, "loads": kgLoads(3)},
			override: map[string]interface{}{"reps": 3},
			edited:   map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 2, "loads": kgLoads(2)},
			fields:   []string{"loads"},
		},
		{
			name:     "left_loads against a shrunk grid",
			base:     map[string]interface{}{"type": "repeater", "hand": "split", "granularity": "rep", "reps": 3, "loads": kgLoads(3), "left_loads": kgLoads(3)},
			override: map[string]interface{}{"left_loads": kgLoads(3)},
			edited:   map[string]interface{}{"type": "repeater", "hand": "split", "granularity": "rep", "reps": 2, "loads": kgLoads(2), "left_loads": kgLoads(2)},
			fields:   []string{"left_loads"},
		},
		{
			// A refusal about two fields at once: either the second hand's loads
			// go or the mode goes back to hanging the hands apart.
			name:     "left_loads under a mode that hangs both hands together",
			base:     map[string]interface{}{"type": "repeater", "hand": "split", "granularity": "uniform", "loads": kgLoads(1), "left_loads": kgLoads(1)},
			override: map[string]interface{}{"left_loads": kgLoads(1)},
			edited:   map[string]interface{}{"type": "repeater", "hand": "both", "granularity": "uniform", "loads": kgLoads(1)},
			fields:   []string{"hand", "left_loads"},
		},
		{
			name:     "hand_positions against a shrunk grid",
			base:     map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 3, "hand_positions": []interface{}{grips(3)}},
			override: map[string]interface{}{"hand_positions": []interface{}{grips(3)}},
			edited:   map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 2, "hand_positions": []interface{}{grips(2)}},
			fields:   []string{"hand_positions"},
		},
		{
			name:     "edge_sizes_mm against a shrunk grid",
			base:     map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 3, "edge_sizes_mm": edges(3)},
			override: map[string]interface{}{"edge_sizes_mm": edges(3)},
			edited:   map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 2, "edge_sizes_mm": edges(2)},
			fields:   []string{"edge_sizes_mm"},
		},
		{
			// The other two field refusal: the open rep count and the percentage
			// cannot both decide how many reps the athlete owes, and the coach
			// answers it by dropping either one.
			name:     "reps_is_max against a rep count read off an assessment",
			base:     map[string]interface{}{"type": "exercise", "reps": 8},
			override: map[string]interface{}{"reps_is_max": true},
			edited:   map[string]interface{}{"type": "exercise", "reps": 8, "variable_targets": repsTarget},
			fields:   []string{"reps_is_max", "variable_targets"},
		},
		{
			name:     "reps_is_max on a retyped block",
			base:     map[string]interface{}{"type": "exercise", "reps": 8},
			override: map[string]interface{}{"reps_is_max": true},
			edited:   map[string]interface{}{"type": "circuit", "reps": 8},
			fields:   []string{"reps_is_max"},
		},
		{
			name:     "interval_seconds on a block that no longer paces itself by a clock",
			base:     map[string]interface{}{"type": "emom", "interval_seconds": 60},
			override: map[string]interface{}{"interval_seconds": 90},
			edited:   map[string]interface{}{"type": "circuit"},
			fields:   []string{"interval_seconds"},
		},
		{
			name:     "rest_seconds on a block retyped to an emom",
			base:     map[string]interface{}{"type": "circuit", "rest_seconds": 60},
			override: map[string]interface{}{"rest_seconds": 90},
			edited:   map[string]interface{}{"type": "emom", "interval_seconds": 60},
			fields:   []string{"rest_seconds"},
		},
		{
			name:     "cycle_rest_seconds on a block retyped to an emom",
			base:     map[string]interface{}{"type": "circuit", "cycle_rest_seconds": 120},
			override: map[string]interface{}{"cycle_rest_seconds": 180},
			edited:   map[string]interface{}{"type": "emom", "interval_seconds": 60},
			fields:   []string{"cycle_rest_seconds"},
		},
		{
			name:     "reps on a block retyped to a single hang",
			base:     map[string]interface{}{"type": "repeater", "reps": 3},
			override: map[string]interface{}{"reps": 2},
			edited:   map[string]interface{}{"type": "hangboard_rep"},
			fields:   []string{"reps"},
		},
		{
			name:     "cycles on a block retyped to a single hang",
			base:     map[string]interface{}{"type": "repeater", "cycles": 3},
			override: map[string]interface{}{"cycles": 2},
			edited:   map[string]interface{}{"type": "hangboard_rep"},
			fields:   []string{"cycles"},
		},
		{
			name:     "cycle_rest_seconds on a block retyped to a single hang",
			base:     map[string]interface{}{"type": "repeater", "cycle_rest_seconds": 120},
			override: map[string]interface{}{"cycle_rest_seconds": 180},
			edited:   map[string]interface{}{"type": "hangboard_rep"},
			fields:   []string{"cycle_rest_seconds"},
		},
	}

	staged := stageRefusalCases(t, app, coachToken, userID, programID, cases)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entry := staged[c.name]
			if entry["override_stale"] != true {
				t.Fatalf("Expected the override flagged stale, got %v", entry)
			}
			if got := staleFieldNames(t, entry); !slices.Equal(got, c.fields) {
				t.Errorf("Expected the refusal attributed to %v, got %v (%v)", c.fields, got, entry["stale_fields"])
			}
			for field, reasons := range staleFieldReasons(t, entry) {
				for _, reason := range reasons {
					if reason == "" {
						t.Errorf("Expected a reason attributed to %s, got %q", field, reason)
					}
				}
			}
		})
	}
}

// An override the item still takes carries no attribution at all, so a reader
// cannot mistake an empty list for a refusal it failed to attribute.
func TestWeekHandler_GetWeek_AttributesNothingToAValidOverride(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, userID, programID := setupAssessmentProgramApp(t, "attribvalid")

	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
	})
	prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemIDs[0], "overrides": map[string]interface{}{"reps": 12}},
		},
	})

	entry := staleOverrideEntry(t, weekOverrideEntries(t, app, weekURL(userID, programID, 1), coachToken), itemIDs[0])
	if entry["override_stale"] != false {
		t.Fatalf("Expected the valid override unflagged, got %v", entry)
	}
	if _, given := entry["stale_fields"]; given {
		t.Errorf("Expected no attribution on a valid override, got %v", entry["stale_fields"])
	}
}

// The multi refusal decision, pinned rather than left incidental: a merged item
// failing for two reasons is attributed for both, so a coach clearing one field
// is told about the other in the same read rather than on the following save.
// The write path is the other half of the decision and answers with the first
// refusal alone, which is the wording it always answered with.
func TestWeekHandler_GetWeek_AttributesEveryReasonAMergedItemFailsFor(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, userID, programID := setupAssessmentProgramApp(t, "attribtwo")

	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "circuit", "rest_seconds": 60, "cycle_rest_seconds": 120},
	})
	itemID := itemIDs[0]

	prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{"rest_seconds": 90, "cycle_rest_seconds": 180}},
		},
	})

	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{"id": itemID, "type": "emom", "interval_seconds": 60},
	})

	week := getWeek(t, app, coachToken, userID, programID, 1)
	entry := staleOverrideEntry(t, weekOverrideEntriesOf(t, week), itemID)
	if entry["override_stale"] != true {
		t.Fatalf("Expected the override flagged stale, got %v", entry)
	}

	reasons := staleFieldReasons(t, entry)
	for _, field := range []string{"rest_seconds", "cycle_rest_seconds"} {
		attributed, named := reasons[field]
		if !named || len(attributed) != 1 {
			t.Fatalf("Expected one reason attributed to %s, got %v", field, reasons)
		}
		if !strings.Contains(attributed[0], field) {
			t.Errorf("Expected the reason attributed to %s to be the one about it, got %q", field, attributed[0])
		}
	}
	if len(reasons) != 2 {
		t.Errorf("Expected both fields attributed and nothing else, got %v", reasons)
	}

	// The write path answers with one refusal, the first, so its wording is
	// what it always was and the coach reads the second one off the week.
	_, message := upsertWeekError(t, app, coachToken, userID, programID, 1, weekUpsertPayloadFromRead(week))
	if !strings.Contains(message, reasons["cycle_rest_seconds"][0]) {
		t.Errorf("Expected the save refused with the first reason, got %q", message)
	}
	if strings.Contains(message, reasons["rest_seconds"][0]) {
		t.Errorf("Expected the save to answer with one refusal rather than both, got %q", message)
	}
}

// insertOverrideDirect writes an override row behind the write path, which is
// the only way to stage the two refusals below: an override naming an
// assessment the coach cannot reference is refused when it is saved, and an
// assessment an override references can be neither deleted nor remeasured while
// that reference stands, so no training edit can make one go stale. The read is
// still what stands between a coach and a marking they cannot explain, so what
// it attributes those refusals to is pinned here.
func insertOverrideDirect(t *testing.T, pool *pgxpool.Pool, sessionID, itemID string, override map[string]interface{}) {
	t.Helper()
	raw, err := json.Marshal(override)
	if err != nil {
		t.Fatalf("Failed to encode the override: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO coach_program_session_overrides (session_id, item_id, overrides) VALUES ($1, $2, $3)`,
		sessionID, itemID, string(raw)); err != nil {
		t.Fatalf("Failed to stage the override: %v", err)
	}
}

func TestWeekHandler_GetWeek_AttributesAnAssessmentRefusalToTheFieldReadingIt(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	setup := setupAssessmentProgram(t, "attribassess")
	app, coachToken := setup.App, setup.CoachToken

	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "duration": 30},
		{"type": "exercise", "duration": 30},
		{"type": "repeater", "hand": "split", "granularity": "uniform"},
	})
	sessionID := prescribeSession(t, app, coachToken, setup.UserID, setup.ProgramID, trainingID, nil)

	const unknown = "11111111-1111-1111-1111-111111111111"
	percentAssessment := []map[string]interface{}{
		{"unit": "percent_assessment", "assessment_id": unknown, "percent": 50, "fallback": 10},
	}
	staged := []struct {
		item     string
		override map[string]interface{}
		field    string
	}{
		{itemIDs[0], map[string]interface{}{"variable_targets": map[string]interface{}{
			"duration": map[string]interface{}{"assessment_id": unknown, "percent": 50, "fallback": 10},
		}}, "variable_targets"},
		{itemIDs[1], map[string]interface{}{"loads": percentAssessment}, "loads"},
		{itemIDs[2], map[string]interface{}{"left_loads": percentAssessment}, "left_loads"},
	}
	for _, one := range staged {
		insertOverrideDirect(t, setup.Pool, sessionID, one.item, one.override)
	}

	entries := weekOverrideEntries(t, app, weekURL(setup.UserID, setup.ProgramID, 1), coachToken)
	for _, one := range staged {
		entry, present := entries[one.item]
		if !present {
			t.Fatalf("Expected the %s override in the coach week, got %v", one.field, entries)
		}
		if entry["override_stale"] != true {
			t.Fatalf("Expected the %s override flagged stale, got %v", one.field, entry)
		}
		if got := staleFieldNames(t, entry); !slices.Equal(got, []string{one.field}) {
			t.Errorf("Expected the refusal attributed to %v, got %v (%v)", one.field, got, entry["stale_fields"])
		}
		reason := staleFieldReasons(t, entry)[one.field][0]
		if !strings.Contains(reason, "unknown assessment_id") {
			t.Errorf("Expected the assessment refusal as the reason on %s, got %q", one.field, reason)
		}
	}
}

// Every field a refusal is attributed to has to be a key the clients read the
// override through, or the editor is handed a name it cannot look up in the row
// it holds. contract/override-keys.json is that list.
func TestAttributedFieldsAreOverrideContractKeys(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, userID, programID := setupAssessmentProgramApp(t, "attribcontract")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)

	contract := map[string]bool{}
	for _, entry := range readOverrideContract(t) {
		contract[entry.Key] = true
	}

	staged := stageRefusalCases(t, app, coachToken, userID, programID, []refusalCase{
		{
			name:     "grid",
			base:     map[string]interface{}{"type": "repeater", "hand": "split", "granularity": "rep", "reps": 3, "loads": kgLoads(3), "left_loads": kgLoads(3), "hand_positions": []interface{}{grips(3), grips(3)}, "edge_sizes_mm": edges(3)},
			override: map[string]interface{}{"loads": kgLoads(3), "left_loads": kgLoads(3), "hand_positions": []interface{}{grips(3), grips(3)}, "edge_sizes_mm": edges(3)},
			edited:   map[string]interface{}{"type": "repeater", "hand": "split", "granularity": "rep", "reps": 2, "loads": kgLoads(2), "left_loads": kgLoads(2), "hand_positions": []interface{}{grips(2), grips(2)}, "edge_sizes_mm": edges(2)},
		},
		{
			name:     "marker",
			base:     map[string]interface{}{"type": "exercise", "reps": 8},
			override: map[string]interface{}{"reps_is_max": true},
			edited: map[string]interface{}{"type": "exercise", "reps": 8, "variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{"assessment_id": assessmentID, "percent": 50, "fallback": 10},
			}},
		},
		{
			name:     "timing",
			base:     map[string]interface{}{"type": "circuit", "rest_seconds": 60, "cycle_rest_seconds": 120},
			override: map[string]interface{}{"rest_seconds": 90, "cycle_rest_seconds": 180},
			edited:   map[string]interface{}{"type": "emom", "interval_seconds": 60},
		},
		{
			name:     "repeat",
			base:     map[string]interface{}{"type": "repeater", "reps": 3, "cycles": 3, "cycle_rest_seconds": 120},
			override: map[string]interface{}{"reps": 2, "cycles": 2, "cycle_rest_seconds": 180},
			edited:   map[string]interface{}{"type": "hangboard_rep"},
		},
	})

	named := map[string]bool{}
	for _, entry := range staged {
		for _, field := range staleFieldNames(t, entry) {
			named[field] = true
		}
	}
	if len(named) == 0 {
		t.Fatalf("Expected the staged refusals to attribute something")
	}
	for field := range named {
		if !contract[field] {
			t.Errorf("A refusal is attributed to %q, which %s does not name, so the editor cannot find it in the override row",
				field, overrideContractPath)
		}
	}
}
