package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxItemCommentLen caps the per-item coach comment length, matching the client input limit.
const maxItemCommentLen = 200

// maxItemDepth caps how deeply items may nest, bounding recursion on untrusted input.
const maxItemDepth = 10

// validItemTypes is the set of accepted training item discriminators.
var validItemTypes = map[string]bool{
	"repeater":      true,
	"hangboard_rep": true,
	"free":          true,
	"exercise":      true,
	"circuit":       true,
	"group":         true,
}

// defaultTrainingType applies when a client omits training_type.
const defaultTrainingType = "workout"

// assessmentUnit is the quantity an assessment result is measured in. A target
// may only reference an assessment measured in the unit of the field it drives,
// otherwise a force result would end up prescribing a duration.
type assessmentUnit string

const (
	unitKilograms   assessmentUnit = "kilograms"
	unitSeconds     assessmentUnit = "seconds"
	unitRepetitions assessmentUnit = "repetitions"
)

// validAssessmentUnits mirrors the assessment_definitions_unit_check constraint
// in schema/schema.sql.
var validAssessmentUnits = map[assessmentUnit]bool{
	unitKilograms:   true,
	unitSeconds:     true,
	unitRepetitions: true,
}

// variableTargetFields maps each scalar item field that may be expressed as a
// percentage of an assessment result to the unit its assessment must carry.
var variableTargetFields = map[string]assessmentUnit{
	"duration": unitSeconds,
	"reps":     unitRepetitions,
}

// percentAssessmentUnit marks a load expressed as a percentage of an assessment
// result. The load value carries the percentage, as it does for percent_bw.
const percentAssessmentUnit = "percent_assessment"

// assessmentUnits answers the unit of every assessment an item may reference. It
// holds the definitions loaded for one request, so the validators stay pure: a
// reference absent from the map is refused, which is also how a definition
// belonging to somebody else is refused, since the query that fills the map only
// ever loads the ones the writer may reference.
type assessmentUnits map[string]assessmentUnit

func (u assessmentUnits) unitOf(id string) (assessmentUnit, bool) {
	unit, ok := u[id]
	return unit, ok
}

// variableTarget references the athlete last result for an assessment. Percent
// applies to that result; fallback is used when the assessment was never done.
type variableTarget struct {
	AssessmentID *string  `json:"assessment_id"`
	Percent      *float64 `json:"percent"`
	Fallback     *float64 `json:"fallback"`
}

// checkAssessment validates the reference itself: a known assessment, measured
// in the unit the field it drives is expressed in.
func (t variableTarget) checkAssessment(field string, want assessmentUnit, units assessmentUnits) error {
	if t.AssessmentID == nil || *t.AssessmentID == "" {
		return fmt.Errorf("%s: missing assessment_id", field)
	}
	got, ok := units.unitOf(*t.AssessmentID)
	if !ok {
		return fmt.Errorf("%s: unknown assessment_id", field)
	}
	if got != want {
		return fmt.Errorf("%s: assessment is measured in %s, not %s", field, got, want)
	}
	return nil
}

func (t variableTarget) validate(field string, want assessmentUnit, units assessmentUnits) error {
	if err := t.checkAssessment(field, want, units); err != nil {
		return err
	}
	if t.Percent == nil || *t.Percent <= 0 {
		return fmt.Errorf("%s: percent must be greater than 0", field)
	}
	if t.Fallback == nil || *t.Fallback < 0 {
		return fmt.Errorf("%s: fallback must be zero or more", field)
	}
	return nil
}

// validateVariableTargets rejects unknown fields and malformed references so a
// client cannot store a target the app would silently drop when resolving it.
func validateVariableTargets(raw json.RawMessage, units assessmentUnits) error {
	if !hasJSONValue(raw) {
		return nil
	}
	var targets map[string]variableTarget
	if err := json.Unmarshal(raw, &targets); err != nil {
		return fmt.Errorf("invalid variable_targets: %w", err)
	}
	for field, target := range targets {
		want, ok := variableTargetFields[field]
		if !ok {
			return fmt.Errorf("invalid variable target field %q", field)
		}
		if err := target.validate(field, want, units); err != nil {
			return err
		}
	}
	return nil
}

// loadWithTarget is the subset of a load entry needed to check assessment
// references. Loads are otherwise stored as opaque JSON.
type loadWithTarget struct {
	Unit string `json:"unit"`
	variableTarget
}

// validateLoads checks the assessment reference of every percent_assessment
// load. Each hand carries its own flat array, one entry per row.
func validateLoads(raw json.RawMessage, units assessmentUnits) error {
	if !hasJSONValue(raw) {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return fmt.Errorf("invalid loads: %w", err)
	}
	for _, entry := range entries {
		var load loadWithTarget
		if err := json.Unmarshal(entry, &load); err != nil {
			return fmt.Errorf("invalid loads: %w", err)
		}
		if load.Unit != percentAssessmentUnit {
			continue
		}
		if err := load.checkAssessment("load", unitKilograms, units); err != nil {
			return err
		}
		if load.Fallback == nil || *load.Fallback < 0 {
			return fmt.Errorf("load: fallback must be zero or more")
		}
	}
	return nil
}

// assessmentRefSource is the part of an item that can carry an assessment
// reference, so the request tree and the frozen response tree share one
// collector instead of drifting apart.
type assessmentRefSource struct {
	VariableTargets json.RawMessage
	Loads           json.RawMessage
	LeftLoads       json.RawMessage
}

func requestRefSources(items []TrainingItemRequest) []assessmentRefSource {
	sources := make([]assessmentRefSource, 0, len(items))
	for _, item := range items {
		sources = append(sources, assessmentRefSource{item.VariableTargets, item.Loads, item.LeftLoads})
		sources = append(sources, requestRefSources(item.Items)...)
	}
	return sources
}

func responseRefSources(items []TrainingItemResponse) []assessmentRefSource {
	sources := make([]assessmentRefSource, 0, len(items))
	for _, item := range items {
		sources = append(sources, assessmentRefSource{item.VariableTargets, item.Loads, item.LeftLoads})
		sources = append(sources, responseRefSources(item.Items)...)
	}
	return sources
}

// collectAssessmentRefs returns every assessment id the sources reference,
// deduplicated and ordered, so one query answers a whole training instead of one
// per item. Malformed JSON is ignored here and reported by the validators.
func collectAssessmentRefs(sources []assessmentRefSource) []string {
	seen := map[string]bool{}
	for _, source := range sources {
		if hasJSONValue(source.VariableTargets) {
			var targets map[string]variableTarget
			if json.Unmarshal(source.VariableTargets, &targets) == nil {
				for _, target := range targets {
					addAssessmentRef(seen, target.AssessmentID)
				}
			}
		}
		for _, raw := range []json.RawMessage{source.Loads, source.LeftLoads} {
			if !hasJSONValue(raw) {
				continue
			}
			var loads []loadWithTarget
			if json.Unmarshal(raw, &loads) != nil {
				continue
			}
			for _, load := range loads {
				addAssessmentRef(seen, load.AssessmentID)
			}
		}
	}
	return sortedKeys(seen)
}

func addAssessmentRef(seen map[string]bool, id *string) {
	if id != nil && *id != "" {
		seen[*id] = true
	}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// resolveAssessmentUnits loads the unit of every assessment the items reference,
// restricted to the ones ownerID may reference. An id absent from the result
// stays absent from the map, so the validators refuse it as unknown whether it
// does not exist or belongs to somebody else.
func resolveAssessmentUnits(ctx context.Context, q *db.Queries, ownerID pgtype.UUID, items []TrainingItemRequest) (assessmentUnits, error) {
	refs := collectAssessmentRefs(requestRefSources(items))
	ids := make([]pgtype.UUID, 0, len(refs))
	for _, ref := range refs {
		var id pgtype.UUID
		// Not a uuid, so it names no definition. Left out of the map and refused
		// as unknown, with the field named, by the validators.
		if err := id.Scan(ref); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return assessmentUnits{}, nil
	}
	rows, err := q.GetAssessmentDefinitionsByIDs(ctx, db.GetAssessmentDefinitionsByIDsParams{
		Ids:    ids,
		UserID: ownerID,
	})
	if err != nil {
		return nil, err
	}
	units := make(assessmentUnits, len(rows))
	for _, row := range rows {
		units[row.ID.String()] = assessmentUnit(row.Unit)
	}
	return units, nil
}

// hasJSONValue reports whether raw holds something other than an absent or null
// JSON value.
func hasJSONValue(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// validTrainingTypes is the set of accepted training_type values. It mirrors the
// trainings_training_type_check constraint in schema/schema.sql.
var validTrainingTypes = map[string]bool{
	"hangboard":  true,
	"climbing":   true,
	"stretching": true,
	"workout":    true,
	"other":      true,
}

// normalizeTrainingType defaults an omitted type and rejects unknown ones.
func normalizeTrainingType(trainingType string) (string, error) {
	if trainingType == "" {
		return defaultTrainingType, nil
	}
	if !validTrainingTypes[trainingType] {
		return "", fmt.Errorf("invalid training type %q", trainingType)
	}
	return trainingType, nil
}

// validateTrainingItems rejects unknown item types, over-deep trees, rep counts
// on a type that does not repeat and malformed configuration arrays before any
// row is written.
func validateTrainingItems(items []TrainingItemRequest, depth int, units assessmentUnits) error {
	if len(items) > 0 && depth > maxItemDepth {
		return fmt.Errorf("items nested more than %d levels deep", maxItemDepth)
	}
	for _, item := range items {
		if !validItemTypes[item.Type] {
			return fmt.Errorf("invalid item type %q", item.Type)
		}
		if err := validateHangboardRepRepeatFields(item); err != nil {
			return err
		}
		if err := validateItemConfiguration(item, units); err != nil {
			return err
		}
		if err := validateTrainingItems(item.Items, depth+1, units); err != nil {
			return err
		}
	}
	return nil
}

// truncateRunes shortens s to at most n runes, preserving multi-byte characters.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

type TrainingHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewTrainingHandler(queries *db.Queries, pool *pgxpool.Pool) *TrainingHandler {
	return &TrainingHandler{queries: queries, pool: pool}
}

func (h *TrainingHandler) ownedTraining() ownedResource[db.Training] {
	return ownedResource[db.Training]{
		label: "Training",
		fetch: h.queries.GetTraining,
		owner: func(t db.Training) pgtype.UUID { return t.UserID },
	}
}

// TrainingItemRequest represents one item in the training tree.
// Circuits and groups carry nested Items; repeaters and hangboard reps are leaves.
type TrainingItemRequest struct {
	Type             string                `json:"type"`
	Cycles           *int32                `json:"cycles"`
	CycleRestSeconds *int32                `json:"cycle_rest_seconds"`
	Reps             *int32                `json:"reps"`
	Duration         *int32                `json:"duration"`
	RestSeconds      *int32                `json:"rest_seconds"`
	ExerciseID       *string               `json:"exercise_id"`
	WorktimeSeconds  *int32                `json:"worktime_seconds"`
	Hand             *string               `json:"hand"        enums:"both,alternate,split,left,right"`
	Granularity      *string               `json:"granularity" enums:"uniform,rep,set"`
	FreeText         *string               `json:"free_text"`
	Comment          *string               `json:"comment"`
	LoadIsMax        bool                  `json:"load_is_max"`
	Loads            json.RawMessage       `json:"loads"           swaggertype:"array,object"`
	LeftLoads        json.RawMessage       `json:"left_loads"      swaggertype:"array,object"`
	HandPositions    json.RawMessage       `json:"hand_positions"  swaggertype:"array,object"`
	EdgeSizesMm      json.RawMessage       `json:"edge_sizes_mm"   swaggertype:"array,integer"`
	VariableTargets  json.RawMessage       `json:"variable_targets" swaggertype:"object"`
	GroupTitle       *string               `json:"group_title"`
	Items            []TrainingItemRequest `json:"items"`
}

type CreateTrainingRequest struct {
	Title        string                `json:"title"`
	Description  *string               `json:"description"`
	TrainingType string                `json:"training_type"`
	Goal         *string               `json:"goal"`
	Comment      *string               `json:"comment"`
	IsFavorite   bool                  `json:"is_favorite"`
	Items        []TrainingItemRequest `json:"items"`
}

type UpdateTrainingRequest struct {
	Title        string                `json:"title"`
	Description  *string               `json:"description"`
	TrainingType string                `json:"training_type"`
	Goal         *string               `json:"goal"`
	Comment      *string               `json:"comment"`
	IsFavorite   bool                  `json:"is_favorite"`
	Items        []TrainingItemRequest `json:"items"`
}

// TrainingItemResponse mirrors TrainingItemRequest with added server-assigned fields.
type TrainingItemResponse struct {
	ID               string                 `json:"id"`
	Type             string                 `json:"type"`
	Position         int32                  `json:"position"`
	Cycles           *int32                 `json:"cycles,omitempty"`
	CycleRestSeconds *int32                 `json:"cycle_rest_seconds,omitempty"`
	Reps             *int32                 `json:"reps,omitempty"`
	Duration         *int32                 `json:"duration,omitempty"`
	RestSeconds      *int32                 `json:"rest_seconds,omitempty"`
	ExerciseID       *string                `json:"exercise_id,omitempty"`
	ExerciseName     *string                `json:"exercise_name,omitempty"`
	WorktimeSeconds  *int32                 `json:"worktime_seconds,omitempty"`
	Hand             *string                `json:"hand,omitempty"        enums:"both,alternate,split,left,right"`
	Granularity      *string                `json:"granularity,omitempty" enums:"uniform,rep,set"`
	FreeText         *string                `json:"free_text,omitempty"`
	Comment          *string                `json:"comment,omitempty"`
	LoadIsMax        bool                   `json:"load_is_max"`
	Loads            json.RawMessage        `json:"loads,omitempty"           swaggertype:"array,object"`
	LeftLoads        json.RawMessage        `json:"left_loads,omitempty"      swaggertype:"array,object"`
	HandPositions    json.RawMessage        `json:"hand_positions,omitempty"  swaggertype:"array,object"`
	EdgeSizesMm      json.RawMessage        `json:"edge_sizes_mm,omitempty"   swaggertype:"array,integer"`
	VariableTargets  json.RawMessage        `json:"variable_targets,omitempty" swaggertype:"object"`
	GroupTitle       *string                `json:"group_title,omitempty"`
	Items            []TrainingItemResponse `json:"items,omitempty"`
}

type TrainingResponse struct {
	ID           string                 `json:"id"`
	UserID       string                 `json:"user_id"`
	Title        string                 `json:"title"`
	Description  *string                `json:"description"`
	TrainingType string                 `json:"training_type"`
	Goal         *string                `json:"goal"`
	Comment      *string                `json:"comment"`
	IsFavorite   bool                   `json:"is_favorite"`
	Items        []TrainingItemResponse `json:"items"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
}

type TrainingListItem struct {
	ID           string  `json:"id"`
	UserID       string  `json:"user_id"`
	Title        string  `json:"title"`
	Description  *string `json:"description"`
	TrainingType string  `json:"training_type"`
	Goal         *string `json:"goal"`
	Comment      *string `json:"comment"`
	IsFavorite   bool    `json:"is_favorite"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func trainingToListItem(s db.Training) TrainingListItem {
	item := TrainingListItem{
		ID:           s.ID.String(),
		UserID:       s.UserID.String(),
		Title:        s.Title,
		TrainingType: s.TrainingType,
		IsFavorite:   s.IsFavorite,
		CreatedAt:    s.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:    s.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if s.Description.Valid {
		item.Description = &s.Description.String
	}
	if s.Goal.Valid {
		item.Goal = &s.Goal.String
	}
	if s.Comment.Valid {
		item.Comment = &s.Comment.String
	}
	return item
}

// insertTrainingItemsRecursive inserts a tree of items into the DB, preserving order and parent links.
func insertTrainingItemsRecursive(
	ctx context.Context,
	queries *db.Queries,
	trainingID pgtype.UUID,
	parentID pgtype.UUID,
	items []TrainingItemRequest,
) ([]TrainingItemResponse, error) {
	result := make([]TrainingItemResponse, 0, len(items))

	for i, req := range items {
		params := db.CreateTrainingItemParams{
			TrainingID: trainingID,
			ParentID:   parentID,
			Type:       req.Type,
			Position:   int32(i),
		}

		if req.Cycles != nil {
			params.Cycles = pgtype.Int4{Int32: *req.Cycles, Valid: true}
		}
		if req.CycleRestSeconds != nil {
			params.CycleRestSeconds = pgtype.Int4{Int32: *req.CycleRestSeconds, Valid: true}
		}
		if req.Reps != nil {
			params.Reps = pgtype.Int4{Int32: *req.Reps, Valid: true}
		}
		if req.Duration != nil {
			params.Duration = pgtype.Int4{Int32: *req.Duration, Valid: true}
		}
		if req.RestSeconds != nil {
			params.RestSeconds = pgtype.Int4{Int32: *req.RestSeconds, Valid: true}
		}
		if req.ExerciseID != nil {
			var exUUID pgtype.UUID
			if err := exUUID.Scan(*req.ExerciseID); err == nil {
				params.ExerciseID = exUUID
			}
		}
		if req.WorktimeSeconds != nil {
			params.WorktimeSeconds = pgtype.Int4{Int32: *req.WorktimeSeconds, Valid: true}
		}
		if req.Hand != nil {
			params.Hand = pgtype.Text{String: *req.Hand, Valid: true}
		}
		if req.Granularity != nil {
			params.Granularity = pgtype.Text{String: *req.Granularity, Valid: true}
		}
		if req.FreeText != nil {
			params.FreeText = pgtype.Text{String: *req.FreeText, Valid: true}
		}
		if req.Comment != nil {
			params.Comment = pgtype.Text{String: truncateRunes(*req.Comment, maxItemCommentLen), Valid: true}
		}
		params.LoadIsMax = req.LoadIsMax
		if hasJSONValue(req.Loads) {
			params.Loads = req.Loads
		}
		if hasJSONValue(req.LeftLoads) {
			params.LeftLoads = req.LeftLoads
		}
		if hasJSONValue(req.HandPositions) {
			params.HandPositions = req.HandPositions
		}
		if hasJSONValue(req.EdgeSizesMm) {
			params.EdgeSizesMm = req.EdgeSizesMm
		}
		if hasJSONValue(req.VariableTargets) {
			params.VariableTargets = req.VariableTargets
		}
		if req.GroupTitle != nil {
			params.GroupTitle = pgtype.Text{String: *req.GroupTitle, Valid: true}
		}

		row, err := queries.CreateTrainingItem(ctx, params)
		if err != nil {
			return nil, err
		}

		resp := dbTrainingItemToResponse(row)

		if len(req.Items) > 0 {
			children, err := insertTrainingItemsRecursive(ctx, queries, trainingID, row.ID, req.Items)
			if err != nil {
				return nil, err
			}
			resp.Items = children
		}

		result = append(result, resp)
	}

	return result, nil
}

func dbTrainingItemToResponse(r db.TrainingItem) TrainingItemResponse {
	resp := TrainingItemResponse{
		ID:       r.ID.String(),
		Type:     r.Type,
		Position: r.Position,
	}
	if r.Cycles.Valid {
		resp.Cycles = &r.Cycles.Int32
	}
	if r.CycleRestSeconds.Valid {
		resp.CycleRestSeconds = &r.CycleRestSeconds.Int32
	}
	if r.Reps.Valid {
		resp.Reps = &r.Reps.Int32
	}
	if r.Duration.Valid {
		resp.Duration = &r.Duration.Int32
	}
	if r.RestSeconds.Valid {
		resp.RestSeconds = &r.RestSeconds.Int32
	}
	if r.ExerciseID.Valid {
		s := r.ExerciseID.String()
		resp.ExerciseID = &s
	}
	if r.WorktimeSeconds.Valid {
		resp.WorktimeSeconds = &r.WorktimeSeconds.Int32
	}
	if r.Hand.Valid {
		resp.Hand = &r.Hand.String
	}
	if r.Granularity.Valid {
		resp.Granularity = &r.Granularity.String
	}
	if r.FreeText.Valid {
		resp.FreeText = &r.FreeText.String
	}
	if r.Comment.Valid {
		resp.Comment = &r.Comment.String
	}
	resp.LoadIsMax = r.LoadIsMax
	if len(r.Loads) > 0 {
		resp.Loads = json.RawMessage(r.Loads)
	}
	if len(r.LeftLoads) > 0 {
		resp.LeftLoads = json.RawMessage(r.LeftLoads)
	}
	if len(r.HandPositions) > 0 {
		resp.HandPositions = json.RawMessage(r.HandPositions)
	}
	if len(r.EdgeSizesMm) > 0 {
		resp.EdgeSizesMm = json.RawMessage(r.EdgeSizesMm)
	}
	if len(r.VariableTargets) > 0 {
		resp.VariableTargets = json.RawMessage(r.VariableTargets)
	}
	if r.GroupTitle.Valid {
		resp.GroupTitle = &r.GroupTitle.String
	}
	return resp
}

// buildTrainingItemTree reconstructs the nested tree from a flat list of DB rows.
// Items are ordered by position and grouped by parent.
func buildTrainingItemTree(rows []db.GetTrainingItemsRow, parentID pgtype.UUID) []TrainingItemResponse {
	result := make([]TrainingItemResponse, 0)
	for _, row := range rows {
		if row.ParentID.Bytes == parentID.Bytes && row.ParentID.Valid == parentID.Valid {
			resp := dbTrainingItemToResponse(trainingItemFromRow(row))
			if row.ExerciseName.Valid {
				resp.ExerciseName = &row.ExerciseName.String
			}
			resp.Items = buildTrainingItemTree(rows, row.ID)
			result = append(result, resp)
		}
	}
	return result
}

// trainingItemFromRow drops the joined exercise_name so the shared response
// mapper can be reused.
func trainingItemFromRow(r db.GetTrainingItemsRow) db.TrainingItem {
	return db.TrainingItem{
		ID:               r.ID,
		TrainingID:       r.TrainingID,
		ParentID:         r.ParentID,
		Type:             r.Type,
		Position:         r.Position,
		Cycles:           r.Cycles,
		CycleRestSeconds: r.CycleRestSeconds,
		Reps:             r.Reps,
		Duration:         r.Duration,
		RestSeconds:      r.RestSeconds,
		ExerciseID:       r.ExerciseID,
		WorktimeSeconds:  r.WorktimeSeconds,
		Hand:             r.Hand,
		Granularity:      r.Granularity,
		FreeText:         r.FreeText,
		Comment:          r.Comment,
		Loads:            r.Loads,
		LeftLoads:        r.LeftLoads,
		HandPositions:    r.HandPositions,
		EdgeSizesMm:      r.EdgeSizesMm,
		LoadIsMax:        r.LoadIsMax,
		VariableTargets:  r.VariableTargets,
		GroupTitle:       r.GroupTitle,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

// CreateCoachTraining godoc
// @Summary Create a training template
// @Description Create a new training template with a structured item tree.
// @Tags Trainings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateTrainingRequest true "Training data"
// @Success 201 {object} TrainingResponse "Training created"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/trainings [post]
func (h *TrainingHandler) CreateTraining(c fiber.Ctx) error {
	userIDStr := middleware.GetUserID(c)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userIDStr); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	var req CreateTrainingRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Title == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Title is required"})
	}

	units, err := resolveAssessmentUnits(c.Context(), h.queries, userUUID, req.Items)
	if err != nil {
		slog.Error("failed to resolve assessment references", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create training"})
	}

	if err := validateTrainingItems(req.Items, 1, units); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	trainingType, err := normalizeTrainingType(req.TrainingType)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	tx, err := h.pool.Begin(c.Context())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create training"})
	}
	defer tx.Rollback(c.Context())

	qtx := h.queries.WithTx(tx)

	params := db.CreateTrainingParams{
		UserID:       userUUID,
		Title:        req.Title,
		TrainingType: trainingType,
		IsFavorite:   req.IsFavorite,
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Goal != nil {
		params.Goal = pgtype.Text{String: *req.Goal, Valid: true}
	}
	if req.Comment != nil {
		params.Comment = pgtype.Text{String: *req.Comment, Valid: true}
	}

	training, err := qtx.CreateTraining(c.Context(), params)
	if err != nil {
		slog.Error("failed to create training", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create training"})
	}

	var zeroParent pgtype.UUID
	items, err := insertTrainingItemsRecursive(c.Context(), qtx, training.ID, zeroParent, req.Items)
	if err != nil {
		slog.Error("failed to insert training items", "training_id", training.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create training items"})
	}

	if err := tx.Commit(c.Context()); err != nil {
		slog.Error("failed to commit training transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize training"})
	}

	return c.Status(fiber.StatusCreated).JSON(buildTrainingResponse(training, items))
}

// GetCoachTrainings godoc
// @Summary List user's training templates
// @Description Get all training templates for the authenticated user (without items).
// @Tags Trainings
// @Produce json
// @Security BearerAuth
// @Success 200 {array} TrainingListItem "List of trainings"
// @Router /api/trainings [get]
func (h *TrainingHandler) GetTrainings(c fiber.Ctx) error {
	userIDStr := middleware.GetUserID(c)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userIDStr); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	trainings, err := h.queries.GetTrainings(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve trainings", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve trainings"})
	}

	result := make([]TrainingListItem, 0, len(trainings))
	for _, s := range trainings {
		result = append(result, trainingToListItem(s))
	}

	return c.Status(fiber.StatusOK).JSON(result)
}

// GetCoachTraining godoc
// @Summary Get a training template
// @Description Get a training template with its full item tree. Only the owner can access.
// @Tags Trainings
// @Produce json
// @Security BearerAuth
// @Param id path string true "Training ID"
// @Success 200 {object} TrainingResponse "Training with items"
// @Failure 400 {object} map[string]string "Invalid training ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Training not found"
// @Router /api/trainings/{id} [get]
func (h *TrainingHandler) GetTraining(c fiber.Ctx) error {
	training, trainingUUID, ok := h.ownedTraining().require(c)
	if !ok {
		return nil
	}

	rows, err := h.queries.GetTrainingItems(c.Context(), trainingUUID)
	if err != nil {
		slog.Error("failed to retrieve training items", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training items"})
	}

	var zeroParent pgtype.UUID
	items := buildTrainingItemTree(rows, zeroParent)

	return c.Status(fiber.StatusOK).JSON(buildTrainingResponse(training, items))
}

// UpdateCoachTraining godoc
// @Summary Update a training template
// @Description Replace the training metadata and items tree. Only the owner can update.
// @Tags Trainings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Training ID"
// @Param request body UpdateTrainingRequest true "Updated training data"
// @Success 200 {object} TrainingResponse "Updated training"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Training not found"
// @Router /api/trainings/{id} [put]
func (h *TrainingHandler) UpdateTraining(c fiber.Ctx) error {
	existing, trainingUUID, ok := h.ownedTraining().require(c)
	if !ok {
		return nil
	}

	var req UpdateTrainingRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Title == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Title is required"})
	}

	units, err := resolveAssessmentUnits(c.Context(), h.queries, existing.UserID, req.Items)
	if err != nil {
		slog.Error("failed to resolve assessment references", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training"})
	}

	if err := validateTrainingItems(req.Items, 1, units); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	trainingType, err := normalizeTrainingType(req.TrainingType)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	tx, err := h.pool.Begin(c.Context())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training"})
	}
	defer tx.Rollback(c.Context())

	qtx := h.queries.WithTx(tx)

	updateParams := db.UpdateTrainingParams{
		ID:           trainingUUID,
		Title:        req.Title,
		TrainingType: trainingType,
		IsFavorite:   req.IsFavorite,
	}
	if req.Description != nil {
		updateParams.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Goal != nil {
		updateParams.Goal = pgtype.Text{String: *req.Goal, Valid: true}
	}
	if req.Comment != nil {
		updateParams.Comment = pgtype.Text{String: *req.Comment, Valid: true}
	}

	training, err := qtx.UpdateTraining(c.Context(), updateParams)
	if err != nil {
		slog.Error("failed to update coach training", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training"})
	}

	if err := qtx.DeleteTrainingItems(c.Context(), trainingUUID); err != nil {
		slog.Error("failed to delete training items", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training items"})
	}

	var zeroParent pgtype.UUID
	items, err := insertTrainingItemsRecursive(c.Context(), qtx, trainingUUID, zeroParent, req.Items)
	if err != nil {
		slog.Error("failed to insert training items", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training items"})
	}

	if err := tx.Commit(c.Context()); err != nil {
		slog.Error("failed to commit training update", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize update"})
	}

	return c.Status(fiber.StatusOK).JSON(buildTrainingResponse(training, items))
}

// scheduledTrainingConstraint is the foreign key that holds a training down
// while a program week still schedules it. Matching it by name keeps the 409
// honest: any other foreign key refusing the delete is not a program, and has
// no business claiming one holds the training.
const scheduledTrainingConstraint = "coach_program_week_sessions_training_id_fkey"

// TrainingProgramUsage names a program that still schedules a training, so a
// client can send the coach straight to the program holding it.
type TrainingProgramUsage struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SessionCount int64  `json:"session_count"`
}

// TrainingInUseResponse is the 409 body returned when a training cannot be
// deleted because programs still schedule it.
type TrainingInUseResponse struct {
	Error    string                 `json:"error"`
	Programs []TrainingProgramUsage `json:"programs"`
}

// DeleteCoachTraining godoc
// @Summary Delete a training template
// @Description Delete a training template and all its items. Only the owner can delete. A training still scheduled by a program cannot be deleted, and the 409 body names the programs holding it.
// @Tags Trainings
// @Produce json
// @Security BearerAuth
// @Param id path string true "Training ID"
// @Success 200 {object} map[string]string "Training deleted"
// @Failure 400 {object} map[string]string "Invalid training ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Training not found"
// @Failure 409 {object} TrainingInUseResponse "Training still scheduled by a program"
// @Router /api/trainings/{id} [delete]
func (h *TrainingHandler) DeleteTraining(c fiber.Ctx) error {
	_, trainingUUID, ok := h.ownedTraining().require(c)
	if !ok {
		return nil
	}

	if err := h.queries.DeleteTraining(c.Context(), trainingUUID); err != nil {
		if constraint, isFK := foreignKeyViolation(err); isFK && constraint == scheduledTrainingConstraint {
			slog.Warn("refused to delete a training a program still schedules", "training_id", trainingUUID.String())
			return h.trainingInUse(c, trainingUUID)
		}
		slog.Error("failed to delete coach training", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete training"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Training deleted successfully"})
}

// trainingInUse answers the refused delete, naming the programs that still
// schedule the training. The lookup runs only on the refusal, so the common
// delete stays a single statement.
func (h *TrainingHandler) trainingInUse(c fiber.Ctx, trainingUUID pgtype.UUID) error {
	rows, err := h.queries.GetProgramsSchedulingTraining(c.Context(), trainingUUID)
	if err != nil {
		slog.Error("failed to list programs scheduling training", "training_id", trainingUUID.String(), "error", err)
		rows = nil
	}

	programs := make([]TrainingProgramUsage, 0, len(rows))
	for _, row := range rows {
		programs = append(programs, TrainingProgramUsage{
			ID:           row.ID.String(),
			Name:         row.Name,
			SessionCount: row.SessionCount,
		})
	}

	return c.Status(fiber.StatusConflict).JSON(TrainingInUseResponse{
		Error:    trainingInUseMessage(programs),
		Programs: programs,
	})
}

func trainingInUseMessage(programs []TrainingProgramUsage) string {
	if len(programs) == 0 {
		return "This training is still scheduled by a program and cannot be deleted"
	}

	quoted := make([]string, 0, len(programs))
	for _, p := range programs {
		quoted = append(quoted, `"`+p.Name+`"`)
	}
	if len(quoted) == 1 {
		return fmt.Sprintf("This training is scheduled by program %s and cannot be deleted", quoted[0])
	}
	return fmt.Sprintf("This training is scheduled by programs %s and cannot be deleted", strings.Join(quoted, ", "))
}

func buildTrainingResponse(training db.Training, items []TrainingItemResponse) TrainingResponse {
	resp := TrainingResponse{
		ID:           training.ID.String(),
		UserID:       training.UserID.String(),
		Title:        training.Title,
		TrainingType: training.TrainingType,
		IsFavorite:   training.IsFavorite,
		Items:        items,
		CreatedAt:    training.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:    training.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if training.Description.Valid {
		resp.Description = &training.Description.String
	}
	if training.Goal.Valid {
		resp.Goal = &training.Goal.String
	}
	if training.Comment.Valid {
		resp.Comment = &training.Comment.String
	}
	if resp.Items == nil {
		resp.Items = []TrainingItemResponse{}
	}
	return resp
}
