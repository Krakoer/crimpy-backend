package handler

import (
	"bytes"
	"crimpy/backend/internal/db"
	"encoding/json"
	"fmt"
)

// itemToRequest reads a stored item back into the shape the validators work on,
// so an override can be checked against the item it targets.
func itemToRequest(item db.TrainingItem) TrainingItemRequest {
	req := TrainingItemRequest{
		Type:            item.Type,
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
	if item.Reps.Valid {
		req.Reps = &item.Reps.Int32
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

// maxItemArrayLen caps how many entries a configuration array may carry, so a
// large declared set and rep count cannot bloat the stored row.
const maxItemArrayLen = 1000

// hangboardItemTypes are the item types that carry a hangboard configuration.
// Only these declare a granularity and a hand mode.
var hangboardItemTypes = map[string]bool{
	"repeater":      true,
	"hangboard_rep": true,
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
		return nil, fmt.Errorf("%s must be an array", field)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	if len(entries) > maxItemArrayLen {
		return nil, fmt.Errorf("%s holds more than %d entries", field, maxItemArrayLen)
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
		return fmt.Errorf("%s holds %d entries but the granularity declares %d rows", field, len(entries), rows)
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
		return fmt.Errorf("hand_positions holds %d hands but the %q mode configures %d", len(hands), hand, wanted)
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
func validateItemArrays(item TrainingItemRequest) error {
	hand := "both"
	if item.Hand != nil {
		hand = *item.Hand
		if !validHands[hand] {
			return fmt.Errorf("invalid hand %q", hand)
		}
	}

	granularity := ""
	if item.Granularity != nil {
		granularity = *item.Granularity
		if !validGranularities[granularity] {
			return fmt.Errorf("invalid granularity %q", granularity)
		}
	}

	configured := hasItemConfiguration(item)
	if granularity == "" {
		if configured && hangboardItemTypes[item.Type] {
			return fmt.Errorf("a configured %s item must declare its granularity", item.Type)
		}
		granularity = "uniform"
	}

	if !worksHandsSeparately(hand) {
		if entries, err := unmarshalItemArray("left_loads", item.LeftLoads); err != nil {
			return err
		} else if entries != nil {
			return fmt.Errorf("left_loads is set but the %q mode hangs both hands together", hand)
		}
	}

	rows := hangboardRowCount(granularity, item.Cycles, item.Reps)
	if rows > maxItemArrayLen {
		return fmt.Errorf("granularity declares more than %d rows", maxItemArrayLen)
	}
	for _, field := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"loads", item.Loads},
		{"left_loads", item.LeftLoads},
		{"edge_sizes_mm", item.EdgeSizesMm},
	} {
		if err := validateRowArray(field.name, field.raw, rows); err != nil {
			return err
		}
	}
	return validateHandPositions(item.HandPositions, hand, rows)
}

// itemOverride is every field a session override may replace on the item it
// targets. It has to name the same keys the clients merge, since an override
// key missing here is silently dropped from the prescription snapshot rather
// than merely being ignored. Kept in step with the app's applyOverride by
// TestOverrideCoversEveryClientKey.
//
// Note hb_worktime_seconds: the override names the item's worktime_seconds
// field with a different key, and the clients read it that way.
type itemOverride struct {
	Cycles           *int32          `json:"cycles"`
	CycleRestSeconds *int32          `json:"cycle_rest_seconds"`
	Reps             *int32          `json:"reps"`
	RestSeconds      *int32          `json:"rest_seconds"`
	WorktimeSeconds  *int32          `json:"hb_worktime_seconds"`
	Hand             *string         `json:"hand"`
	Granularity      *string         `json:"granularity"`
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
	reps             **int32
	restSeconds      **int32
	worktimeSeconds  **int32
	hand             **string
	granularity      **string
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
		reps:             &i.Reps,
		restSeconds:      &i.RestSeconds,
		worktimeSeconds:  &i.WorktimeSeconds,
		hand:             &i.Hand,
		granularity:      &i.Granularity,
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
		reps:             &i.Reps,
		restSeconds:      &i.RestSeconds,
		worktimeSeconds:  &i.WorktimeSeconds,
		hand:             &i.Hand,
		granularity:      &i.Granularity,
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
		{over.Reps, target.reps},
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
// layout no client could read: resizing the grid without resending the
// configuration arrays it invalidates, or shipping arrays that disagree with
// the granularity in force once the override is applied.
func validateItemOverride(base TrainingItemRequest, raw json.RawMessage) error {
	merged, err := applyItemOverride(base, raw)
	if err != nil {
		return err
	}
	return validateItemConfiguration(merged)
}

// validateItemConfiguration checks everything about a single item that has to
// hold however the item was written, whether directly on the training or
// through a session override merged onto it.
func validateItemConfiguration(item TrainingItemRequest) error {
	if err := validateItemArrays(item); err != nil {
		return err
	}
	if err := validateVariableTargets(item.VariableTargets); err != nil {
		return err
	}
	for _, loads := range []json.RawMessage{item.Loads, item.LeftLoads} {
		if err := validateLoads(loads); err != nil {
			return err
		}
	}
	return nil
}
