package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
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

// attributedRefusal is one stale_fields entry as the read hands it over.
type attributedRefusal struct{ field, reason string }

// staleFieldPairs reads stale_fields off a coach week override entry, the
// unattributed entry included: the empty field is the whole override answer,
// which has a test of its own, so every other reader here wants the fatal
// staleFieldReasons keeps.
func staleFieldPairs(t *testing.T, entry map[string]interface{}) []attributedRefusal {
	t.Helper()
	attributed, told := entry["stale_fields"].([]interface{})
	if !told {
		t.Fatalf("Expected stale_fields on the flagged override, got %v", entry)
	}
	if len(attributed) == 0 {
		t.Fatalf("Expected stale_fields to attribute the refusal, got %v", entry)
	}
	pairs := make([]attributedRefusal, 0, len(attributed))
	for _, one := range attributed {
		refusal, ok := one.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected an attributed refusal object, got %v", one)
		}
		field, carried := refusal["field"].(string)
		if !carried {
			t.Fatalf("Expected the refusal to carry a field, empty or not, got %v", refusal)
		}
		reason, given := refusal["reason"].(string)
		if !given || reason == "" {
			t.Fatalf("Expected the refusal to carry its reason beside the field, got %v", refusal)
		}
		pairs = append(pairs, attributedRefusal{field: field, reason: reason})
	}
	return pairs
}

// staleFieldReasons reads the attribution off a coach week override entry, as
// the reasons attributed to each field. An unattributed refusal is fatal here on
// purpose: attribution is total, so a validator that ever stopped attributing
// one should fail these tests rather than quietly reduce them to the fallback.
func staleFieldReasons(t *testing.T, entry map[string]interface{}) map[string][]string {
	t.Helper()
	byField := map[string][]string{}
	for _, refusal := range staleFieldPairs(t, entry) {
		if refusal.field == "" {
			t.Fatalf("Expected the refusal attributed to a field, got %v", entry["stale_fields"])
		}
		byField[refusal.field] = append(byField[refusal.field], refusal.reason)
	}
	return byField
}

// namedFieldsTheRowCarries applies to a flagged entry the rule the read
// documents for a reader deciding whether a refusal still stands: of the fields
// a refusal names, keep the ones the override row carries and skip the rest,
// since a name the row does not carry is absent again after every edit and
// counting it as unchanged would hold the marking up forever. What is left is
// what the coach can move to clear it, and an empty answer means the refusal is
// about the row as a whole.
func namedFieldsTheRowCarries(t *testing.T, entry map[string]interface{}) []string {
	t.Helper()
	row, _ := entry["overrides"].(map[string]interface{})
	carried := map[string]bool{}
	for _, refusal := range staleFieldPairs(t, entry) {
		if _, present := row[refusal.field]; present {
			carried[refusal.field] = true
		}
	}
	return slices.Sorted(maps.Keys(carried))
}

// staleFieldNames is every field the refusals of one override are attributed to.
func staleFieldNames(t *testing.T, entry map[string]interface{}) []string {
	t.Helper()
	return slices.Sorted(maps.Keys(staleFieldReasons(t, entry)))
}

// assertRefusedWithOneAttributedReason checks a refused save was answered with
// one of the wordings the read hands over. One, not all of them: the read hands
// over every reason a merged item is refused for and the write path answers with
// the first alone, so asserting the message carries every reason only holds
// while a fixture happens to yield a single reason spread over several fields.
// What the property is really about is that the coach reads one wording whether
// they are told why a save was refused or why a saved override stopped applying.
func assertRefusedWithOneAttributedReason(t *testing.T, message string, reasons map[string][]string) {
	t.Helper()
	handedOver := []string{}
	for _, attributed := range reasons {
		handedOver = append(handedOver, attributed...)
	}
	if slices.ContainsFunc(handedOver, func(reason string) bool { return strings.Contains(message, reason) }) {
		return
	}
	t.Errorf("Expected the save refused with one of the reasons the read attributes, %v, got %q", slices.Sorted(slices.Values(handedOver)), message)
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

// eachRefusalCase is every refusal a stale override can carry, staged one per
// item, with the fields it is attributed to. The list is the one
// Krakoer/crimpy#100 enumerates: a named configuration array, the AMRAP marker,
// the emom timing fields, and the fields that describe a repeated hang. It is a
// function rather than a table so the contract test below can read the field
// names it declares without staging anything, which is why the assessment id it
// takes only has to be a well formed one there.
func eachRefusalCase(assessmentID string) []refusalCase {
	repsTarget := map[string]interface{}{
		"reps": map[string]interface{}{"assessment_id": assessmentID, "percent": 50, "fallback": 10},
	}

	return []refusalCase{
		{
			// An array measured against a row count it disagrees with is
			// attributed to the array and to the three fields the count is read
			// from, since either side of the disagreement is a field the coach
			// can move and the row count is not a field of its own.
			name:     "loads against a shrunk grid",
			base:     map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 3, "loads": kgLoads(3)},
			override: map[string]interface{}{"loads": kgLoads(3)},
			edited:   map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 2, "loads": kgLoads(2)},
			fields:   []string{"cycles", "granularity", "loads", "reps"},
		},
		{
			// The one the editor cannot answer from the override row alone: the
			// refused array is the item's own, which the override never carried,
			// so a reader has to be told the name rather than look for a key.
			// The layout fields the row count is read from are named beside it,
			// which is what leaves the coach a field of the row to act on:
			// TestWeekHandler_GetWeek_LeavesAnActionableFieldOnARefusalAboutTheItemsArray.
			name:     "an array the override does not carry",
			base:     map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 3, "loads": kgLoads(3)},
			override: map[string]interface{}{"reps": 3},
			edited:   map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 2, "loads": kgLoads(2)},
			fields:   []string{"cycles", "granularity", "loads", "reps"},
		},
		{
			name:     "left_loads against a shrunk grid",
			base:     map[string]interface{}{"type": "repeater", "hand": "split", "granularity": "rep", "reps": 3, "loads": kgLoads(3), "left_loads": kgLoads(3)},
			override: map[string]interface{}{"left_loads": kgLoads(3)},
			edited:   map[string]interface{}{"type": "repeater", "hand": "split", "granularity": "rep", "reps": 2, "loads": kgLoads(2), "left_loads": kgLoads(2)},
			fields:   []string{"cycles", "granularity", "left_loads", "reps"},
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
			fields:   []string{"cycles", "granularity", "hand_positions", "reps"},
		},
		{
			name:     "edge_sizes_mm against a shrunk grid",
			base:     map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 3, "edge_sizes_mm": edges(3)},
			override: map[string]interface{}{"edge_sizes_mm": edges(3)},
			edited:   map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 2, "edge_sizes_mm": edges(2)},
			fields:   []string{"cycles", "edge_sizes_mm", "granularity", "reps"},
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
}

func TestWeekHandler_GetWeek_AttributesEachRefusalToItsField(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, userID, programID := setupAssessmentProgramApp(t, "attribfield")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)
	cases := eachRefusalCase(assessmentID)

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
	wanted := fmt.Sprintf("session 0 override on item %s: %s", itemID, reasons["cycle_rest_seconds"][0])
	if message != wanted {
		t.Errorf("Expected the save refused with %q, got %q", wanted, message)
	}
	// The same weaker property the single refusal test asserts, checked here on
	// a fixture that genuinely carries two reasons, so that assertion cannot
	// quietly become one only a single reason fixture can pass.
	assertRefusedWithOneAttributedReason(t, message, reasons)
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
	insertRawOverrideDirect(t, pool, sessionID, itemID, string(raw))
}

// insertRawOverrideDirect writes the override column verbatim, so a test can
// stage a stored value no payload could put there at all.
func insertRawOverrideDirect(t *testing.T, pool *pgxpool.Pool, sessionID, itemID, raw string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO coach_program_session_overrides (session_id, item_id, overrides) VALUES ($1, $2, $3)`,
		sessionID, itemID, raw); err != nil {
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

// The stuck marking Krakoer/crimpy#100 is really about, staged end to end and
// read the way the read documents a reader should read it. A week prescribed
// with {"reps": 3} on an item the coach later shrank to two rows is refused for
// a row count the override resizes, so the refusal names the item's own loads,
// which the row does not carry, beside the layout fields the count is read from.
// A reader counting an absent name as unchanged would mark this block forever,
// including through the reps edit that clears the refusal, which is why the rule
// is to skip the names the row does not carry. What is left has to be a field of
// the row, or the coach is told a block is refused and handed nothing of it to
// act on.
func TestWeekHandler_GetWeek_LeavesAnActionableFieldOnARefusalAboutTheItemsArray(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, userID, programID := setupAssessmentProgramApp(t, "attribstuck")

	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "repeater", "granularity": "rep", "reps": 3, "loads": kgLoads(3)},
	})
	itemID := itemIDs[0]

	prescribeSession(t, app, coachToken, userID, programID, trainingID, map[string]interface{}{
		"overrides": []map[string]interface{}{
			{"item_id": itemID, "overrides": map[string]interface{}{"reps": 3}},
		},
	})
	editStaleOverrideTraining(t, app, coachToken, trainingID, []map[string]interface{}{
		{"id": itemID, "type": "repeater", "granularity": "rep", "reps": 2, "loads": kgLoads(2)},
	})

	week := getWeek(t, app, coachToken, userID, programID, 1)
	entry := staleOverrideEntry(t, weekOverrideEntriesOf(t, week), itemID)
	if entry["override_stale"] != true {
		t.Fatalf("Expected the override flagged stale, got %v", entry)
	}
	named := staleFieldNames(t, entry)
	if !slices.Contains(named, "loads") {
		t.Errorf("Expected the item's own array among the fields named, got %v", named)
	}
	carried := namedFieldsTheRowCarries(t, entry)
	if !slices.Equal(carried, []string{"reps"}) {
		t.Fatalf("Expected the reader left with reps to act on, got %v out of %v", carried, named)
	}

	// The marking is not stale itself: the week the coach just read is still
	// refused, so a reader keeping it up is right to.
	if status, message := upsertWeekError(t, app, coachToken, userID, programID, 1, weekUpsertPayloadFromRead(week)); status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 saving the week the coach just read, got %d: %s", status, message)
	}

	// And it clears on the one field the reader was left with, which is the edit
	// that makes the block saveable again.
	sessionID := week["sessions"].([]interface{})[0].(map[string]interface{})["id"]
	cleared := upsertWeekSessions(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{
			"id":          sessionID,
			"training_id": trainingID,
			"day_of_week": 0,
			"overrides": []map[string]interface{}{
				{"item_id": itemID, "overrides": map[string]interface{}{"reps": 2}},
			},
		}},
	})
	if echoed := staleOverrideEntry(t, weekOverrideEntriesOf(t, cleared), itemID); echoed["override_stale"] != false {
		t.Errorf("Expected setting the override's reps to clear the marking, got %v", echoed)
	}
}

// The empty field the read promises, produced rather than assumed. An override
// that is not an object is refused with nothing left to attribute the refusal
// to, since there is no merged item to ask anything of, so the read answers with
// the documented fallback: one entry, no field, the reason beside it. It is also
// where a reader arrives from the other end, when a refusal names only fields
// the row does not carry and the skip rule leaves it nothing, which is the case
// TestWeekHandler_GetWeek_LeavesAnActionableFieldOnARefusalAboutTheItemsArray
// asserts it is not in.
func TestWeekHandler_GetWeek_AttributesAnUnattributableRefusalToTheWholeOverride(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	setup := setupAssessmentProgram(t, "attribwhole")
	app, coachToken := setup.App, setup.CoachToken

	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
	})
	itemID := itemIDs[0]
	sessionID := prescribeSession(t, app, coachToken, setup.UserID, setup.ProgramID, trainingID, nil)

	// Behind the write path, which refuses this shape wherever it arrives. The
	// read is still what stands between a coach and a marking nothing explains.
	insertRawOverrideDirect(t, setup.Pool, sessionID, itemID, `[12]`)

	entries := weekOverrideEntriesOf(t, getWeek(t, app, coachToken, setup.UserID, setup.ProgramID, 1))
	entry, present := entries[itemID]
	if !present {
		t.Fatalf("Expected the override in the coach week, got %v", entries)
	}
	if entry["override_stale"] != true {
		t.Fatalf("Expected the override flagged stale, got %v", entry)
	}
	pairs := staleFieldPairs(t, entry)
	if len(pairs) != 1 || pairs[0].field != "" {
		t.Fatalf("Expected the one refusal attributed to nothing, got %v", entry["stale_fields"])
	}
	if pairs[0].reason != "overrides must be an object" {
		t.Errorf("Expected the reason beside the empty field, got %q", pairs[0].reason)
	}
	if carried := namedFieldsTheRowCarries(t, entry); len(carried) != 0 {
		t.Errorf("Expected the refusal left to the row as a whole, got %v", carried)
	}
}

// Every field a refusal is attributed to has to be a key the clients read the
// override through, or the editor is handed a name it cannot look up in the row
// it holds. contract/override-keys.json is that list, and it deliberately
// spells one key differently from the item field it replaces,
// hb_worktime_seconds for worktime_seconds, which is how a refusal attributed
// with an item field spelling would ship a name the portal cannot find.
//
// The whole refusal table is held to the contract rather than a handful of
// cases staged here, because a staged case only proves the property for the
// fields it happens to name and the ones it missed are exactly where that
// mismatch would hide. TestWeekHandler_GetWeek_AttributesEachRefusalToItsField
// pins every refusal against these same names, so holding the names to the
// contract holds the refusals to it, and with no database round trip.
func TestAttributedFieldsAreOverrideContractKeys(t *testing.T) {
	contract := map[string]bool{}
	for _, entry := range readOverrideContract(t) {
		contract[entry.Key] = true
	}

	named := map[string]bool{}
	for _, c := range eachRefusalCase("11111111-1111-1111-1111-111111111111") {
		if len(c.fields) == 0 {
			t.Errorf("The %q case declares no attribution, so nothing of it is held to %s", c.name, overrideContractPath)
		}
		for _, field := range c.fields {
			named[field] = true
		}
	}
	if len(named) == 0 {
		t.Fatalf("Expected the refusal table to name some field")
	}
	for _, field := range slices.Sorted(maps.Keys(named)) {
		if !contract[field] {
			t.Errorf("A refusal is attributed to %q, which %s does not name, so the editor cannot find it in the override row",
				field, overrideContractPath)
		}
	}
}

// The one refusal wording no training payload can reach, so it is pinned where
// it is answered: an override that is not an object is refused on the override
// path, which reaches the item columns without a training item to write them on.
// It is also the refusal nothing can attribute, whose read side is
// TestWeekHandler_GetWeek_AttributesAnUnattributableRefusalToTheWholeOverride.
func TestWeekHandler_UpsertWeek_KeepsTheWordingOfAnOverrideThatIsNotAnObject(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, userID, programID := setupAssessmentProgramApp(t, "wordingover")

	trainingID, itemIDs := createStaleOverrideTraining(t, app, coachToken, []map[string]interface{}{
		{"type": "exercise", "reps": 8},
	})
	itemID := itemIDs[0]

	status, message := upsertWeekError(t, app, coachToken, userID, programID, 1, map[string]interface{}{
		"sessions": []map[string]interface{}{{
			"training_id": trainingID,
			"day_of_week": 0,
			"overrides": []map[string]interface{}{
				{"item_id": itemID, "overrides": []interface{}{12}},
			},
		}},
	})
	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 saving an override that is not an object, got %d: %s", status, message)
	}
	wanted := fmt.Sprintf("session 0 override on item %s: overrides must be an object", itemID)
	if message != wanted {
		t.Errorf("Expected the refusal to read %q, got %q", wanted, message)
	}
}

// Attribution rides beside the wording rather than replacing it, so what a
// client is told when a write is refused does not move. Existing tests assert
// these refusals by substring, which would not catch a rewording, so the exact
// wording of every refusal the direct training write path answers with is
// pinned here: this change carries a refusal's fields, and nothing about how it
// reads.
//
// Two are left out on purpose, invalid variable_targets and invalid loads: what
// they read past the prefix is the json decoder's message about a Go type, so
// pinning them would pin the decoder rather than a wording of ours.
func TestTrainingHandler_KeepsEveryRefusalWordingWhileAttributingIt(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken, _, _, _ := setupAssessmentProgramApp(t, "wording")

	_, assessmentID := createAssessmentTraining(t, app, coachToken, "Pull up max", "repetitions", false)
	_, loadAssessmentID := createAssessmentTraining(t, app, coachToken, "Max hang test", "kilograms", false)
	const unknownAssessmentID = "11111111-1111-1111-1111-111111111111"

	for _, refused := range []struct {
		name    string
		item    map[string]interface{}
		wording string
	}{
		{
			name:    "a repeat field on a single hang",
			item:    map[string]interface{}{"type": "hangboard_rep", "reps": 4},
			wording: "a hangboard_rep is a single hang and takes no reps; use a repeater to repeat a hang",
		},
		{
			name:    "an interval on a block that is not an emom",
			item:    map[string]interface{}{"type": "circuit", "interval_seconds": 60},
			wording: "only an emom takes an interval_seconds; a circuit paces itself by its rests",
		},
		{
			name:    "an emom with no interval",
			item:    map[string]interface{}{"type": "emom"},
			wording: "an emom must declare the interval_seconds its rounds start on",
		},
		{
			name:    "an emom with an interval of nothing",
			item:    map[string]interface{}{"type": "emom", "interval_seconds": 0},
			wording: "interval_seconds must be at least 1 second",
		},
		{
			// Two rests at once, so what the write path answers with when a
			// merged item fails twice is pinned as the first refusal alone.
			name:    "an emom carrying both rests",
			item:    map[string]interface{}{"type": "emom", "interval_seconds": 60, "rest_seconds": 30, "cycle_rest_seconds": 90},
			wording: "an emom rests for whatever is left of its interval and takes no cycle_rest_seconds",
		},
		{
			name:    "an open rep count on a block that lays its rows out",
			item:    map[string]interface{}{"type": "circuit", "reps_is_max": true},
			wording: "only an exercise takes reps_is_max; a circuit lays its configuration out one row per rep and needs the count",
		},
		{
			name: "an open rep count read off an assessment",
			item: map[string]interface{}{"type": "exercise", "reps_is_max": true, "variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{"assessment_id": assessmentID, "percent": 50, "fallback": 10},
			}},
			wording: "reps_is_max leaves the rep count open and cannot also be a percentage of an assessment",
		},
		{
			name:    "an unknown hand",
			item:    map[string]interface{}{"type": "repeater", "hand": "sideways"},
			wording: `invalid hand "sideways"`,
		},
		{
			name:    "an unknown granularity",
			item:    map[string]interface{}{"type": "repeater", "granularity": "hourly"},
			wording: `invalid granularity "hourly"`,
		},
		{
			name:    "a configured block declaring no layout",
			item:    map[string]interface{}{"type": "repeater", "loads": kgLoads(1)},
			wording: "a configured repeater item must declare its granularity",
		},
		{
			name:    "a second hand's loads under a mode that hangs both hands together",
			item:    map[string]interface{}{"type": "repeater", "hand": "both", "granularity": "uniform", "loads": kgLoads(1), "left_loads": kgLoads(1)},
			wording: `left_loads is set but the "both" mode hangs both hands together`,
		},
		{
			name:    "an array that is not an array",
			item:    map[string]interface{}{"type": "repeater", "granularity": "uniform", "loads": map[string]interface{}{"value": 10}},
			wording: "loads must be an array",
		},
		{
			name:    "an array that disagrees with the layout",
			item:    map[string]interface{}{"type": "repeater", "granularity": "rep", "reps": 2, "loads": kgLoads(3)},
			wording: "loads holds 3 entries but the granularity declares 2 rows",
		},
		{
			name:    "too many grip arrays for the mode",
			item:    map[string]interface{}{"type": "repeater", "granularity": "uniform", "hand": "both", "hand_positions": []interface{}{grips(1), grips(1)}},
			wording: `hand_positions holds 2 hands but the "both" mode configures 1`,
		},
		{
			name: "a target reading an assessment measured in something else",
			item: map[string]interface{}{"type": "exercise", "duration": 30, "variable_targets": map[string]interface{}{
				"duration": map[string]interface{}{"assessment_id": assessmentID, "percent": 50, "fallback": 10},
			}},
			wording: "duration: assessment is measured in repetitions, not seconds",
		},
		{
			name: "a load reading an assessment nothing names",
			item: map[string]interface{}{"type": "exercise", "loads": []map[string]interface{}{
				{"unit": "percent_assessment", "assessment_id": unknownAssessmentID, "percent": 50, "fallback": 10},
			}},
			wording: "load: unknown assessment_id",
		},
		{
			name:    "a repeat count on a single hang",
			item:    map[string]interface{}{"type": "hangboard_rep", "cycles": 2},
			wording: "a hangboard_rep is a single hang and takes no cycles; use a repeater to repeat a hang",
		},
		{
			name:    "a rest between repeats on a single hang",
			item:    map[string]interface{}{"type": "hangboard_rep", "cycle_rest_seconds": 90},
			wording: "a hangboard_rep is a single hang and takes no cycle_rest_seconds; use a repeater to repeat a hang",
		},
		{
			name:    "an array longer than the cap",
			item:    map[string]interface{}{"type": "repeater", "granularity": "uniform", "loads": kgLoads(1001)},
			wording: "loads holds more than 1000 entries",
		},
		{
			name:    "a layout declaring more rows than the cap",
			item:    map[string]interface{}{"type": "repeater", "granularity": "set", "cycles": 100, "reps": 100},
			wording: "granularity declares more than 1000 rows",
		},
		{
			name: "a target on a field no target drives",
			item: map[string]interface{}{"type": "exercise", "variable_targets": map[string]interface{}{
				"grip": map[string]interface{}{"assessment_id": assessmentID, "percent": 50, "fallback": 10},
			}},
			wording: `invalid variable target field "grip"`,
		},
		{
			name: "a target reading an assessment nothing names",
			item: map[string]interface{}{"type": "exercise", "reps": 8, "variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{"assessment_id": unknownAssessmentID, "percent": 50, "fallback": 10},
			}},
			wording: "reps: unknown assessment_id",
		},
		{
			name: "a target taking no share of its assessment",
			item: map[string]interface{}{"type": "exercise", "reps": 8, "variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{"assessment_id": assessmentID, "percent": 0, "fallback": 10},
			}},
			wording: "reps: percent must be greater than 0",
		},
		{
			name: "a target falling back to less than nothing",
			item: map[string]interface{}{"type": "exercise", "reps": 8, "variable_targets": map[string]interface{}{
				"reps": map[string]interface{}{"assessment_id": assessmentID, "percent": 50, "fallback": -1},
			}},
			wording: "reps: fallback must be zero or more",
		},
		{
			name: "a load falling back to less than nothing",
			item: map[string]interface{}{"type": "exercise", "loads": []map[string]interface{}{
				{"unit": "percent_assessment", "assessment_id": loadAssessmentID, "percent": 50, "fallback": -1},
			}},
			wording: "load: fallback must be zero or more",
		},
	} {
		t.Run(refused.name, func(t *testing.T) {
			status, body := postJSON(t, app, "/api/trainings", coachToken, map[string]interface{}{
				"title":         "Wording",
				"training_type": "climbing",
				"items":         []map[string]interface{}{refused.item},
			})
			if status != fiber.StatusBadRequest {
				t.Fatalf("Expected 400, got %d: %v", status, body)
			}
			if got := fmt.Sprint(body["error"]); got != refused.wording {
				t.Errorf("Expected the refusal to read %q, got %q", refused.wording, got)
			}
		})
	}
}
