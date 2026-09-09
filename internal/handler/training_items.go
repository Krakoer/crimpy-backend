package handler

import (
	"bytes"
	"context"
	"crimpy/backend/internal/db"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// itemToRequest reads a stored item back into the shape the validators work on,
// so an override can be checked against the item it targets.
func itemToRequest(item db.TrainingItem) TrainingItemRequest {
	req := TrainingItemRequest{
		Type:            item.Type,
		RepsIsMax:       item.RepsIsMax,
		LoadIsMax:       item.LoadIsMax,
		Loads:           json.RawMessage(item.Loads),
		LeftLoads:       json.RawMessage(item.LeftLoads),
		HandPositions:   json.RawMessage(item.HandPositions),
		EdgeSizesMm:     json.RawMessage(item.EdgeSizesMm),
		VariableTargets: json.RawMessage(item.VariableTargets),
	}
	if item.Cycles.Valid {
		req.Cycles = &item.Cycles.Int32
	}
	if item.CycleRestSeconds.Valid {
		req.CycleRestSeconds = &item.CycleRestSeconds.Int32
	}
	if item.IntervalSeconds.Valid {
		req.IntervalSeconds = &item.IntervalSeconds.Int32
	}
	if item.Reps.Valid {
		req.Reps = &item.Reps.Int32
	}
	if item.Duration.Valid {
		req.Duration = &item.Duration.Int32
	}
	if item.RestSeconds.Valid {
		req.RestSeconds = &item.RestSeconds.Int32
	}
	if item.WorktimeSeconds.Valid {
		req.WorktimeSeconds = &item.WorktimeSeconds.Int32
	}
	if item.Hand.Valid {
		req.Hand = &item.Hand.String
	}
	if item.Granularity.Valid {
		req.Granularity = &item.Granularity.String
	}
	return req
}

// itemResponseToRequest reads a prescription item back into the shape the
// validators work on, so a stored override can be checked against the item as
// it now stands. It carries the type and the overridable fields, which is
// everything the item validators read.
func itemResponseToRequest(item TrainingItemResponse) TrainingItemRequest {
	return TrainingItemRequest{
		Type:             item.Type,
		Cycles:           item.Cycles,
		CycleRestSeconds: item.CycleRestSeconds,
		IntervalSeconds:  item.IntervalSeconds,
		Reps:             item.Reps,
		RepsIsMax:        item.RepsIsMax,
		Duration:         item.Duration,
		RestSeconds:      item.RestSeconds,
		WorktimeSeconds:  item.WorktimeSeconds,
		Hand:             item.Hand,
		Granularity:      item.Granularity,
		LoadIsMax:        item.LoadIsMax,
		Loads:            item.Loads,
		LeftLoads:        item.LeftLoads,
		HandPositions:    item.HandPositions,
		EdgeSizesMm:      item.EdgeSizesMm,
		VariableTargets:  item.VariableTargets,
	}
}

// maxItemArrayLen caps how many entries a configuration array may carry, so a
// large declared set and rep count cannot bloat the stored row.
const maxItemArrayLen = 1000

// hangboardItemTypes are the item types that carry a hangboard configuration.
// Only these declare a granularity and a hand mode.
var hangboardItemTypes = map[string]bool{
	"repeater":      true,
	"hangboard_rep": true,
}

// itemRefusal is one reason a merged item is refused, together with the
// override keys the refusal is about. The keys are what a coach client needs in
// order to tell an edit of the refused field from an edit of another field of
// the same override row: the whole row is what is stored, and the server only
// ever refuses part of it. They are spelled as contract/override-keys.json
// spells them, and no field at all stands for the override as a whole.
type itemRefusal struct {
	fields []string
	err    error
}

func (r itemRefusal) Error() string { return r.err.Error() }

func (r itemRefusal) Unwrap() error { return r.err }

// refusedFields names the override keys a refusal is about. It wraps the reason
// rather than formatting it, so the wording every write path answers with stays
// written where it always was, and it answers nil for a check that passed, so a
// call site can attribute a refusal another function decided.
func refusedFields(err error, fields ...string) error {
	if err == nil {
		return nil
	}
	return itemRefusal{fields: fields, err: err}
}

// itemRefusals is every reason one merged item is refused, in the order the
// validators ask. Its message is the first refusal alone, because a write path
// answers with one refusal and this is not the change that rewords them, while
// the coach read hands over all of them: the reader's question is per field, so
// reporting only the first would have a coach fixing one field discover the next
// on the following save.
type itemRefusals []itemRefusal

func (rs itemRefusals) Error() string {
	if len(rs) == 0 {
		return ""
	}
	return rs[0].Error()
}

// add flattens err into the list, so a validator that already collected several
// refusals contributes each of them rather than one opaque entry. An error
// nothing attributed is kept with no field, which reads as the whole override
// and is the answer a reader falls back to.
func (rs itemRefusals) add(err error) itemRefusals {
	if err == nil {
		return rs
	}
	switch refusal := err.(type) {
	case itemRefusals:
		return append(rs, refusal...)
	case itemRefusal:
		return append(rs, refusal)
	default:
		return append(rs, itemRefusal{err: err})
	}
}

// orNil returns the list as an error, and nil when it holds none, since an empty
// list would otherwise read as a refusal to every caller.
func (rs itemRefusals) orNil() error {
	if len(rs) == 0 {
		return nil
	}
	return rs
}

// itemRefusalsOf reads back the refusals a validator answered with.
func itemRefusalsOf(err error) itemRefusals {
	var none itemRefusals
	return none.add(err)
}

// errHangboardRepField is the one wording every write path answers with, so a
// caller reads the same refusal whether the field arrived on the item or through
// a session override.
func errHangboardRepField(field string) error {
	return refusedFields(fmt.Errorf("a hangboard_rep is a single hang and takes no %s; use a repeater to repeat a hang", field), field)
}

// hangboardRepRepeatFields names every field describing a repeated hang that an
// item carries, in the order they are refused.
func hangboardRepRepeatFields(reps, cycles, cycleRestSeconds *int32) []string {
	var fields []string
	for _, field := range []struct {
		name  string
		value *int32
	}{
		{"reps", reps},
		{"cycles", cycles},
		{"cycle_rest_seconds", cycleRestSeconds},
	} {
		if field.value != nil {
			fields = append(fields, field.name)
		}
	}
	return fields
}

// errHangboardRepFields refuses every repeat field the item carries. Each is a
// field the coach has to clear, so all of them are named rather than only the
// first, and the write paths still answer with the first alone.
func errHangboardRepFields(fields []string) error {
	var refusals itemRefusals
	for _, field := range fields {
		refusals = refusals.add(errHangboardRepField(field))
	}
	return refusals.orNil()
}

// validateHangboardRepRepeatFields keeps the two hangboard types apart. A
// hangboard_rep is one hang, and every client expands it to exactly one, so a
// stored rep or cycle count is read back and never run. The repeater is the
// block that repeats a hang, and it takes all three.
func validateHangboardRepRepeatFields(item TrainingItemRequest) error {
	if item.Type != "hangboard_rep" {
		return nil
	}
	return errHangboardRepFields(hangboardRepRepeatFields(item.Reps, item.Cycles, item.CycleRestSeconds))
}

// validateOverrideHangboardRepRepeatFields closes the same hole on the override
// path, which reaches the columns without going through
// validateHangboardRepRepeatFields. It answers on what the caller sent rather
// than on the merged item, so what the targeted row already holds cannot make an
// override fail.
func validateOverrideHangboardRepRepeatFields(base TrainingItemRequest, raw json.RawMessage) error {
	if base.Type != "hangboard_rep" || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var over itemOverride
	if err := json.Unmarshal(raw, &over); err != nil {
		return fmt.Errorf("overrides must be an object")
	}
	return errHangboardRepFields(hangboardRepRepeatFields(over.Reps, over.Cycles, over.CycleRestSeconds))
}

// validateEmomFields keeps the emom and the circuit apart. What makes a block
// every minute on the minute is its interval, so an emom must carry one and no
// other type may: an interval on a circuit names a clock nothing runs it
// against. The two rest fields go the other way, since the leftover of the
// interval is already the rest of an emom round: stored, they would read as a
// gap the block never plays.
func validateEmomFields(item TrainingItemRequest) error {
	if item.Type != "emom" {
		if item.IntervalSeconds != nil {
			return refusedFields(fmt.Errorf("only an emom takes an interval_seconds; a %s paces itself by its rests", item.Type), "interval_seconds")
		}
		return nil
	}
	var refusals itemRefusals
	switch {
	case item.IntervalSeconds == nil:
		refusals = refusals.add(refusedFields(fmt.Errorf("an emom must declare the interval_seconds its rounds start on"), "interval_seconds"))
	case *item.IntervalSeconds < 1:
		refusals = refusals.add(refusedFields(fmt.Errorf("interval_seconds must be at least 1 second"), "interval_seconds"))
	}
	for _, field := range []struct {
		name  string
		value *int32
	}{
		{"cycle_rest_seconds", item.CycleRestSeconds},
		{"rest_seconds", item.RestSeconds},
	} {
		if field.value != nil {
			refusals = refusals.add(refusedFields(fmt.Errorf("an emom rests for whatever is left of its interval and takes no %s", field.name), field.name))
		}
	}
	return refusals.orNil()
}

// validateRepsIsMax checks the AMRAP marker. It stands in for the rep count, so
// it belongs only to a type that has one to leave open, and a rep count already
// set as a percentage of an assessment is a number: the two cannot both decide
// how many reps the athlete owes.
func validateRepsIsMax(item TrainingItemRequest) error {
	if !item.RepsIsMax {
		return nil
	}
	if item.Type != "exercise" {
		return refusedFields(fmt.Errorf("only an exercise takes reps_is_max; a %s lays its configuration out one row per rep and needs the count", item.Type), "reps_is_max")
	}
	if !hasJSONValue(item.VariableTargets) {
		return nil
	}
	var targets map[string]variableTarget
	if err := json.Unmarshal(item.VariableTargets, &targets); err != nil {
		return nil
	}
	if _, ok := targets["reps"]; ok {
		return refusedFields(fmt.Errorf("reps_is_max leaves the rep count open and cannot also be a percentage of an assessment"), "reps_is_max", "variable_targets")
	}
	return nil
}

var validHands = map[string]bool{
	"both":      true,
	"alternate": true,
	"split":     true,
	"left":      true,
	"right":     true,
}

var validGranularities = map[string]bool{
	"uniform": true,
	"rep":     true,
	"set":     true,
}

// worksHandsSeparately reports whether the mode hangs one hand at a time, so
// each hand carries its own loads and grips. The clients share this rule.
func worksHandsSeparately(hand string) bool {
	return hand == "alternate" || hand == "split"
}

// hangboardHandCount is how many grip arrays an item stores: one per hand when
// the hands are worked separately, one shared array otherwise.
func hangboardHandCount(hand string) int {
	if worksHandsSeparately(hand) {
		return 2
	}
	return 1
}

// hangboardRowCount is how many configuration rows an item carries: one for a
// uniform item, one per rep, or one per set and rep. Every client derives the
// same count from the same three fields.
func hangboardRowCount(granularity string, cycles, reps *int32) int {
	count := func(value *int32) int {
		if value == nil || *value < 1 {
			return 1
		}
		return int(*value)
	}
	switch granularity {
	case "set":
		return count(cycles) * count(reps)
	case "rep":
		return count(reps)
	default:
		return 1
	}
}

// unmarshalItemArray rejects a configuration field that is not a JSON array and
// returns its entries. An absent, null or empty field yields no entries and no
// error: every configuration array is optional, and an empty array is the
// natural encoding for "no values".
func unmarshalItemArray(field string, raw json.RawMessage) ([]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, refusedFields(fmt.Errorf("%s must be an array", field), field)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	if len(entries) > maxItemArrayLen {
		return nil, refusedFields(fmt.Errorf("%s holds more than %d entries", field, maxItemArrayLen), field)
	}
	return entries, nil
}

// validateRowArray checks a configuration array that holds one entry per row.
func validateRowArray(field string, raw json.RawMessage, rows int) error {
	entries, err := unmarshalItemArray(field, raw)
	if err != nil {
		return err
	}
	if entries != nil && len(entries) != rows {
		return refusedFields(fmt.Errorf("%s holds %d entries but the granularity declares %d rows", field, len(entries), rows), field)
	}
	return nil
}

// validateHandPositions checks the grips, which carry one array of rows per
// hand rather than one entry per row. A mode that hangs both hands together
// carries a single shared array.
func validateHandPositions(raw json.RawMessage, hand string, rows int) error {
	hands, err := unmarshalItemArray("hand_positions", raw)
	if err != nil {
		return err
	}
	if hands == nil {
		return nil
	}
	if wanted := hangboardHandCount(hand); len(hands) > wanted {
		return refusedFields(fmt.Errorf("hand_positions holds %d hands but the %q mode configures %d", len(hands), hand, wanted), "hand_positions", "hand")
	}
	for _, hand := range hands {
		if err := validateRowArray("hand_positions", hand, rows); err != nil {
			return err
		}
	}
	return nil
}

// hasItemConfiguration reports whether the item carries any hangboard
// configuration array at all.
func hasItemConfiguration(item TrainingItemRequest) bool {
	for _, raw := range []json.RawMessage{item.Loads, item.LeftLoads, item.HandPositions, item.EdgeSizesMm} {
		entries, err := unmarshalItemArray("", raw)
		if err != nil || entries != nil {
			return true
		}
	}
	return false
}

// validateItemArrays checks that the hand and granularity are known, that an
// item carrying a configuration declares the layout it is written in, and that
// every array matches both that layout and the hand mode.
//
// The layout checks answer alone: a hand, a granularity or a row count nothing
// can read decides what every array below is measured against, so carrying on
// would only pile refusals about the arrays onto a layout the coach has to fix
// first. The array checks are independent of each other and are all collected,
// so a reader attributing them is told about every array at once.
func validateItemArrays(item TrainingItemRequest) error {
	hand := "both"
	if item.Hand != nil {
		hand = *item.Hand
		if !validHands[hand] {
			return refusedFields(fmt.Errorf("invalid hand %q", hand), "hand")
		}
	}

	granularity := ""
	if item.Granularity != nil {
		granularity = *item.Granularity
		if !validGranularities[granularity] {
			return refusedFields(fmt.Errorf("invalid granularity %q", granularity), "granularity")
		}
	}

	configured := hasItemConfiguration(item)
	if granularity == "" {
		if configured && hangboardItemTypes[item.Type] {
			return refusedFields(fmt.Errorf("a configured %s item must declare its granularity", item.Type), "granularity")
		}
		granularity = "uniform"
	}

	var refusals itemRefusals
	// A left_loads the hand mode leaves no room for is refused whole, so it is
	// not also measured against a row count it has no business having.
	leftLoadsRefused := false
	if !worksHandsSeparately(hand) {
		entries, err := unmarshalItemArray("left_loads", item.LeftLoads)
		switch {
		case err != nil:
			refusals = refusals.add(err)
			leftLoadsRefused = true
		case entries != nil:
			refusals = refusals.add(refusedFields(fmt.Errorf("left_loads is set but the %q mode hangs both hands together", hand), "left_loads", "hand"))
			leftLoadsRefused = true
		}
	}

	rows := hangboardRowCount(granularity, item.Cycles, item.Reps)
	if rows > maxItemArrayLen {
		return refusals.add(refusedFields(fmt.Errorf("granularity declares more than %d rows", maxItemArrayLen), "granularity", "cycles", "reps")).orNil()
	}
	for _, field := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"loads", item.Loads},
		{"left_loads", item.LeftLoads},
		{"edge_sizes_mm", item.EdgeSizesMm},
	} {
		if field.name == "left_loads" && leftLoadsRefused {
			continue
		}
		refusals = refusals.add(validateRowArray(field.name, field.raw, rows))
	}
	return refusals.add(validateHandPositions(item.HandPositions, hand, rows)).orNil()
}

// itemOverride is every field a session override may replace on the item it
// targets. It has to name the same keys the clients merge, since an override
// key missing there is silently dropped from the prescription snapshot rather
// than merely being ignored. contract/override-keys.json is the list all three
// clients are held to: TestOverrideCoversEveryContractKey asserts these json
// tags against it, and the app and the portal vendor the same file and assert
// their own merge against it. A key added here is added to that file, and the
// copy in crimpy-app and crimpy-frontend refreshed, or their suites fail.
//
// Note hb_worktime_seconds: the override names the item's worktime_seconds
// field with a different key, and the clients read it that way.
type itemOverride struct {
	Cycles           *int32          `json:"cycles"`
	CycleRestSeconds *int32          `json:"cycle_rest_seconds"`
	IntervalSeconds  *int32          `json:"interval_seconds"`
	Reps             *int32          `json:"reps"`
	RepsIsMax        *bool           `json:"reps_is_max"`
	Duration         *int32          `json:"duration"`
	RestSeconds      *int32          `json:"rest_seconds"`
	WorktimeSeconds  *int32          `json:"hb_worktime_seconds"`
	Hand             *string         `json:"hand"`
	Granularity      *string         `json:"granularity"`
	LoadIsMax        *bool           `json:"load_is_max"`
	Loads            json.RawMessage `json:"loads"`
	LeftLoads        json.RawMessage `json:"left_loads"`
	HandPositions    json.RawMessage `json:"hand_positions"`
	EdgeSizesMm      json.RawMessage `json:"edge_sizes_mm"`
	VariableTargets  json.RawMessage `json:"variable_targets"`
}

// overridableItem points at the fields an override may replace. The request and
// response item shapes each expose one, so a session override is merged the same
// way whichever shape holds the item.
type overridableItem struct {
	cycles           **int32
	cycleRestSeconds **int32
	intervalSeconds  **int32
	reps             **int32
	repsIsMax        *bool
	duration         **int32
	restSeconds      **int32
	worktimeSeconds  **int32
	hand             **string
	granularity      **string
	loadIsMax        *bool
	loads            *json.RawMessage
	leftLoads        *json.RawMessage
	handPositions    *json.RawMessage
	edgeSizesMm      *json.RawMessage
	variableTargets  *json.RawMessage
}

func (i *TrainingItemRequest) overridable() overridableItem {
	return overridableItem{
		cycles:           &i.Cycles,
		cycleRestSeconds: &i.CycleRestSeconds,
		intervalSeconds:  &i.IntervalSeconds,
		reps:             &i.Reps,
		repsIsMax:        &i.RepsIsMax,
		duration:         &i.Duration,
		restSeconds:      &i.RestSeconds,
		worktimeSeconds:  &i.WorktimeSeconds,
		hand:             &i.Hand,
		granularity:      &i.Granularity,
		loadIsMax:        &i.LoadIsMax,
		loads:            &i.Loads,
		leftLoads:        &i.LeftLoads,
		handPositions:    &i.HandPositions,
		edgeSizesMm:      &i.EdgeSizesMm,
		variableTargets:  &i.VariableTargets,
	}
}

func (i *TrainingItemResponse) overridable() overridableItem {
	return overridableItem{
		cycles:           &i.Cycles,
		cycleRestSeconds: &i.CycleRestSeconds,
		intervalSeconds:  &i.IntervalSeconds,
		reps:             &i.Reps,
		repsIsMax:        &i.RepsIsMax,
		duration:         &i.Duration,
		restSeconds:      &i.RestSeconds,
		worktimeSeconds:  &i.WorktimeSeconds,
		hand:             &i.Hand,
		granularity:      &i.Granularity,
		loadIsMax:        &i.LoadIsMax,
		loads:            &i.Loads,
		leftLoads:        &i.LeftLoads,
		handPositions:    &i.HandPositions,
		edgeSizesMm:      &i.EdgeSizesMm,
		variableTargets:  &i.VariableTargets,
	}
}

// isEmptyJSONArray reports whether raw is the empty array. An override carrying
// one prescribes nothing, so the clients leave the base value in place rather
// than wiping it, and the merge has to agree.
func isEmptyJSONArray(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "[]"
}

// mergeItemOverride merges an override into the item it targets, field by
// field, the way the clients apply it.
func mergeItemOverride(target overridableItem, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var over itemOverride
	if err := json.Unmarshal(raw, &over); err != nil {
		return fmt.Errorf("overrides must be an object")
	}
	for _, field := range []struct {
		override *int32
		target   **int32
	}{
		{over.Cycles, target.cycles},
		{over.CycleRestSeconds, target.cycleRestSeconds},
		{over.IntervalSeconds, target.intervalSeconds},
		{over.Reps, target.reps},
		{over.Duration, target.duration},
		{over.RestSeconds, target.restSeconds},
		{over.WorktimeSeconds, target.worktimeSeconds},
	} {
		if field.override != nil {
			*field.target = field.override
		}
	}
	for _, field := range []struct {
		override *string
		target   **string
	}{
		{over.Hand, target.hand},
		{over.Granularity, target.granularity},
	} {
		if field.override != nil {
			*field.target = field.override
		}
	}
	// The two markers are plain booleans on the item and pointers here, so an
	// override that leaves one out is told apart from one that turns it off.
	for _, field := range []struct {
		override *bool
		target   *bool
	}{
		{over.RepsIsMax, target.repsIsMax},
		{over.LoadIsMax, target.loadIsMax},
	} {
		if field.override != nil {
			*field.target = *field.override
		}
	}
	for _, field := range []struct {
		override json.RawMessage
		target   *json.RawMessage
		// An empty array is a value for variable_targets, which is an object,
		// and a no-op for the layout arrays.
		keepBaseWhenEmpty bool
	}{
		{over.Loads, target.loads, true},
		{over.LeftLoads, target.leftLoads, true},
		{over.HandPositions, target.handPositions, true},
		{over.EdgeSizesMm, target.edgeSizesMm, true},
		{over.VariableTargets, target.variableTargets, false},
	} {
		if len(field.override) == 0 || string(field.override) == "null" {
			continue
		}
		if field.keepBaseWhenEmpty && isEmptyJSONArray(field.override) {
			continue
		}
		*field.target = field.override
	}
	return nil
}

// applyItemOverride returns the item as the override leaves it.
func applyItemOverride(base TrainingItemRequest, raw json.RawMessage) (TrainingItemRequest, error) {
	merged := base
	if err := mergeItemOverride(merged.overridable(), raw); err != nil {
		return base, err
	}
	return merged, nil
}

// validateItemOverride rejects an override that leaves the item it targets in a
// state no client could read: resizing the grid without resending the
// configuration arrays it invalidates, shipping arrays that disagree with the
// granularity in force once the override is applied, or reaching through the
// override keys to a shape the direct write path refuses. An override carries
// interval_seconds, reps_is_max, rest_seconds, cycle_rest_seconds and
// variable_targets, which is enough to put a clock on a circuit, a rest on an
// emom and a percentage on an open rep count, so the item invariants are checked
// on the merged item and not only where it was written.
func validateItemOverride(base TrainingItemRequest, raw json.RawMessage, units assessmentUnits) error {
	refusals := itemRefusalsOf(validateOverrideHangboardRepRepeatFields(base, raw))
	merged, err := applyItemOverride(base, raw)
	if err != nil {
		// An override that is not an object is refused whole, and there is no
		// merged item left to ask anything else about. The check above answers
		// with the same wording for the same reason, so it is not repeated.
		return itemRefusalsOf(err).orNil()
	}
	refusals = refusals.add(validateEmomFields(merged))
	refusals = refusals.add(validateRepsIsMax(merged))
	return refusals.add(validateItemConfiguration(merged, units)).orNil()
}

// staleOverrides names every override the item it targets no longer takes,
// keyed as the caller keyed bases and raw, with every reason it is refused and
// the override keys each reason is about. It is the single place the base,
// apply, resolve and validate sequence lives, so the week write path, the
// athlete read and the snapshot cannot drift into judging the same override
// differently. The key is the caller's own handle on an override, an item id
// where the overrides are already keyed by one and a position where they are
// not, and a base with no entry in raw is checked against an absent override.
func staleOverrides[K comparable](ctx context.Context, q *db.Queries, ownerID pgtype.UUID, bases map[K]TrainingItemRequest, raw map[K]json.RawMessage) (map[K]itemRefusals, error) {
	if len(bases) == 0 {
		return nil, nil
	}

	// An override replaces the loads and the targets wholesale, so the
	// assessments to resolve are the ones the merged items reference, not the
	// ones the training already held. Reading the base instead would let an
	// override name a definition the coach cannot reference.
	merged := make([]TrainingItemRequest, 0, len(bases))
	for key, base := range bases {
		applied, err := applyItemOverride(base, raw[key])
		if err != nil {
			applied = base
		}
		merged = append(merged, applied)
	}

	units, err := resolveAssessmentUnits(ctx, q, ownerID, merged)
	if err != nil {
		return nil, err
	}

	var stale map[K]itemRefusals
	for key, base := range bases {
		if err := validateItemOverride(base, raw[key], units); err != nil {
			if stale == nil {
				stale = make(map[K]itemRefusals, len(bases))
			}
			stale[key] = itemRefusalsOf(err)
		}
	}
	return stale, nil
}

// validateItemConfiguration checks everything about a single item that has to
// hold however the item was written, whether directly on the training or
// through a session override merged onto it.
func validateItemConfiguration(item TrainingItemRequest, units assessmentUnits) error {
	refusals := itemRefusalsOf(validateItemArrays(item))
	refusals = refusals.add(validateVariableTargets(item.VariableTargets, units))
	for _, loads := range []struct {
		field string
		raw   json.RawMessage
	}{
		{"loads", item.Loads},
		{"left_loads", item.LeftLoads},
	} {
		refusals = refusals.add(validateLoads(loads.field, loads.raw, units))
	}
	return refusals.orNil()
}
