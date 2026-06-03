package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CoachTrainingHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewCoachTrainingHandler(queries *db.Queries, pool *pgxpool.Pool) *CoachTrainingHandler {
	return &CoachTrainingHandler{queries: queries, pool: pool}
}

// TrainingItemRequest represents one item in the training tree.
// Circuits and sections carry nested Items; repeaters and hangboard reps are leaves.
type TrainingItemRequest struct {
	Type             string                `json:"type"`
	Cycles           *int32                `json:"cycles"`
	CycleRestSeconds *int32                `json:"cycle_rest_seconds"`
	Reps             *int32                `json:"reps"`
	Duration         *int32                `json:"duration"`
	RestSeconds      *int32                `json:"rest_seconds"`
	ExerciseID       *string               `json:"exercise_id"`
	WorktimeSeconds  *int32                `json:"worktime_seconds"`
	Hand             *string               `json:"hand"`
	FreeText         *string               `json:"free_text"`
	LoadIsMax        bool                  `json:"load_is_max"`
	Loads            json.RawMessage       `json:"loads"           swaggertype:"array,object"`
	LeftLoads        json.RawMessage       `json:"left_loads"      swaggertype:"array,object"`
	HandPositions    json.RawMessage       `json:"hand_positions"  swaggertype:"array,object"`
	EdgeSizesMm      json.RawMessage       `json:"edge_sizes_mm"   swaggertype:"array,integer"`
	SectionTitle     *string               `json:"section_title"`
	Items            []TrainingItemRequest `json:"items"`
}

type CreateCoachTrainingRequest struct {
	Title        string                `json:"title"`
	Description  *string               `json:"description"`
	TrainingType string                `json:"training_type"`
	Goal         string                `json:"goal"`
	Comment      string                `json:"comment"`
	Items        []TrainingItemRequest `json:"items"`
}

type UpdateCoachTrainingRequest struct {
	Title        string                `json:"title"`
	Description  *string               `json:"description"`
	TrainingType string                `json:"training_type"`
	Goal         string                `json:"goal"`
	Comment      string                `json:"comment"`
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
	WorktimeSeconds  *int32                 `json:"worktime_seconds,omitempty"`
	Hand             *string                `json:"hand,omitempty"`
	FreeText         *string                `json:"free_text,omitempty"`
	LoadIsMax        bool                   `json:"load_is_max"`
	Loads            json.RawMessage        `json:"loads,omitempty"           swaggertype:"array,object"`
	LeftLoads        json.RawMessage        `json:"left_loads,omitempty"      swaggertype:"array,object"`
	HandPositions    json.RawMessage        `json:"hand_positions,omitempty"  swaggertype:"array,object"`
	EdgeSizesMm      json.RawMessage        `json:"edge_sizes_mm,omitempty"   swaggertype:"array,integer"`
	SectionTitle     *string                `json:"section_title,omitempty"`
	Items            []TrainingItemResponse `json:"items,omitempty"`
}

type CoachTrainingResponse struct {
	ID           string                 `json:"id"`
	CoachID      string                 `json:"coach_id"`
	Title        string                 `json:"title"`
	Description  *string                `json:"description"`
	TrainingType string                 `json:"training_type"`
	Goal         string                 `json:"goal"`
	Comment      string                 `json:"comment"`
	Items        []TrainingItemResponse `json:"items"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
}

type CoachTrainingListItem struct {
	ID           string  `json:"id"`
	CoachID      string  `json:"coach_id"`
	Title        string  `json:"title"`
	Description  *string `json:"description"`
	TrainingType string  `json:"training_type"`
	Goal         string  `json:"goal"`
	Comment      string  `json:"comment"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func coachTrainingToListItem(s db.CoachTraining) CoachTrainingListItem {
	item := CoachTrainingListItem{
		ID:           s.ID.String(),
		CoachID:      s.CoachID.String(),
		Title:        s.Title,
		TrainingType: s.TrainingType,
		Goal:         s.Goal,
		Comment:      s.Comment,
		CreatedAt:    s.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:    s.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if s.Description.Valid {
		item.Description = &s.Description.String
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
		params := db.CreateCoachTrainingItemParams{
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
		if req.FreeText != nil {
			params.FreeText = pgtype.Text{String: *req.FreeText, Valid: true}
		}
		params.LoadIsMax = req.LoadIsMax
		if len(req.Loads) > 0 && string(req.Loads) != "null" {
			params.Loads = req.Loads
		}
		if len(req.LeftLoads) > 0 && string(req.LeftLoads) != "null" {
			params.LeftLoads = req.LeftLoads
		}
		if len(req.HandPositions) > 0 && string(req.HandPositions) != "null" {
			params.HandPositions = req.HandPositions
		}
		if len(req.EdgeSizesMm) > 0 && string(req.EdgeSizesMm) != "null" {
			params.EdgeSizesMm = req.EdgeSizesMm
		}
		if req.SectionTitle != nil {
			params.SectionTitle = pgtype.Text{String: *req.SectionTitle, Valid: true}
		}

		row, err := queries.CreateCoachTrainingItem(ctx, params)
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

func dbTrainingItemToResponse(r db.CoachTrainingItem) TrainingItemResponse {
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
	if r.FreeText.Valid {
		resp.FreeText = &r.FreeText.String
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
	if r.SectionTitle.Valid {
		resp.SectionTitle = &r.SectionTitle.String
	}
	return resp
}

// buildTrainingItemTree reconstructs the nested tree from a flat list of DB rows.
// Items are ordered by position and grouped by parent.
func buildTrainingItemTree(rows []db.CoachTrainingItem, parentID pgtype.UUID) []TrainingItemResponse {
	result := make([]TrainingItemResponse, 0)
	for _, row := range rows {
		if row.ParentID.Bytes == parentID.Bytes && row.ParentID.Valid == parentID.Valid {
			resp := dbTrainingItemToResponse(row)
			resp.Items = buildTrainingItemTree(rows, row.ID)
			result = append(result, resp)
		}
	}
	return result
}

// CreateCoachTraining godoc
// @Summary Create a coach training template
// @Description Create a new training template with a structured item tree. Requires a validated coach account.
// @Tags CoachTrainings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateCoachTrainingRequest true "Training data"
// @Success 201 {object} CoachTrainingResponse "Training created"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/coach/trainings [post]
func (h *CoachTrainingHandler) CreateCoachTraining(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var req CreateCoachTrainingRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Title == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Title is required"})
	}

	tx, err := h.pool.Begin(context.Background())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create training"})
	}
	defer tx.Rollback(context.Background())

	qtx := h.queries.WithTx(tx)

	if req.TrainingType == "" {
		req.TrainingType = "workout"
	}
	params := db.CreateCoachTrainingParams{
		CoachID:      coachUUID,
		Title:        req.Title,
		TrainingType: req.TrainingType,
		Goal:         req.Goal,
		Comment:      req.Comment,
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}

	training, err := qtx.CreateCoachTraining(context.Background(), params)
	if err != nil {
		slog.Error("failed to create coach training", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create training"})
	}

	var zeroParent pgtype.UUID
	items, err := insertTrainingItemsRecursive(context.Background(), qtx, training.ID, zeroParent, req.Items)
	if err != nil {
		slog.Error("failed to insert training items", "training_id", training.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create training items"})
	}

	if err := tx.Commit(context.Background()); err != nil {
		slog.Error("failed to commit training transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize training"})
	}

	return c.Status(fiber.StatusCreated).JSON(buildCoachTrainingResponse(training, items))
}

// GetCoachTrainings godoc
// @Summary List coach's training templates
// @Description Get all training templates for the authenticated coach (without items).
// @Tags CoachTrainings
// @Produce json
// @Security BearerAuth
// @Success 200 {array} CoachTrainingListItem "List of trainings"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Router /api/coach/trainings [get]
func (h *CoachTrainingHandler) GetCoachTrainings(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	trainings, err := h.queries.GetCoachTrainings(context.Background(), coachUUID)
	if err != nil {
		slog.Error("failed to retrieve coach trainings", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve trainings"})
	}

	result := make([]CoachTrainingListItem, 0, len(trainings))
	for _, s := range trainings {
		result = append(result, coachTrainingToListItem(s))
	}

	return c.Status(fiber.StatusOK).JSON(result)
}

// GetCoachTraining godoc
// @Summary Get a coach training template
// @Description Get a training template with its full item tree. Only the owning coach can access.
// @Tags CoachTrainings
// @Produce json
// @Security BearerAuth
// @Param id path string true "Training ID"
// @Success 200 {object} CoachTrainingResponse "Training with items"
// @Failure 400 {object} map[string]string "Invalid training ID"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Training not found"
// @Router /api/coach/trainings/{id} [get]
func (h *CoachTrainingHandler) GetCoachTraining(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var trainingUUID pgtype.UUID
	if err := trainingUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}

	training, err := h.queries.GetCoachTraining(context.Background(), trainingUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Training not found"})
		}
		slog.Error("failed to retrieve coach training", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training"})
	}

	if training.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	rows, err := h.queries.GetCoachTrainingItems(context.Background(), trainingUUID)
	if err != nil {
		slog.Error("failed to retrieve training items", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training items"})
	}

	var zeroParent pgtype.UUID
	items := buildTrainingItemTree(rows, zeroParent)

	return c.Status(fiber.StatusOK).JSON(buildCoachTrainingResponse(training, items))
}

// UpdateCoachTraining godoc
// @Summary Update a coach training template
// @Description Replace the training metadata and items tree. Only the owning coach can update.
// @Tags CoachTrainings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Training ID"
// @Param request body UpdateCoachTrainingRequest true "Updated training data"
// @Success 200 {object} CoachTrainingResponse "Updated training"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Training not found"
// @Router /api/coach/trainings/{id} [put]
func (h *CoachTrainingHandler) UpdateCoachTraining(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var trainingUUID pgtype.UUID
	if err := trainingUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}

	existing, err := h.queries.GetCoachTraining(context.Background(), trainingUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Training not found"})
		}
		slog.Error("failed to retrieve coach training for update", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training"})
	}

	if existing.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	var req UpdateCoachTrainingRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Title == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Title is required"})
	}

	tx, err := h.pool.Begin(context.Background())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training"})
	}
	defer tx.Rollback(context.Background())

	qtx := h.queries.WithTx(tx)

	if req.TrainingType == "" {
		req.TrainingType = "workout"
	}
	updateParams := db.UpdateCoachTrainingParams{
		ID:           trainingUUID,
		Title:        req.Title,
		TrainingType: req.TrainingType,
		Goal:         req.Goal,
		Comment:      req.Comment,
	}
	if req.Description != nil {
		updateParams.Description = pgtype.Text{String: *req.Description, Valid: true}
	}

	training, err := qtx.UpdateCoachTraining(context.Background(), updateParams)
	if err != nil {
		slog.Error("failed to update coach training", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training"})
	}

	if err := qtx.DeleteCoachTrainingItems(context.Background(), trainingUUID); err != nil {
		slog.Error("failed to delete training items", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training items"})
	}

	var zeroParent pgtype.UUID
	items, err := insertTrainingItemsRecursive(context.Background(), qtx, trainingUUID, zeroParent, req.Items)
	if err != nil {
		slog.Error("failed to insert training items", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training items"})
	}

	if err := tx.Commit(context.Background()); err != nil {
		slog.Error("failed to commit training update", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize update"})
	}

	return c.Status(fiber.StatusOK).JSON(buildCoachTrainingResponse(training, items))
}

// DeleteCoachTraining godoc
// @Summary Delete a coach training template
// @Description Delete a training template and all its items. Only the owning coach can delete.
// @Tags CoachTrainings
// @Produce json
// @Security BearerAuth
// @Param id path string true "Training ID"
// @Success 200 {object} map[string]string "Training deleted"
// @Failure 400 {object} map[string]string "Invalid training ID"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Training not found"
// @Router /api/coach/trainings/{id} [delete]
func (h *CoachTrainingHandler) DeleteCoachTraining(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var trainingUUID pgtype.UUID
	if err := trainingUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}

	training, err := h.queries.GetCoachTraining(context.Background(), trainingUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Training not found"})
		}
		slog.Error("failed to retrieve coach training for delete", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training"})
	}

	if training.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteCoachTraining(context.Background(), trainingUUID); err != nil {
		slog.Error("failed to delete coach training", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete training"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Training deleted successfully"})
}

func buildCoachTrainingResponse(training db.CoachTraining, items []TrainingItemResponse) CoachTrainingResponse {
	resp := CoachTrainingResponse{
		ID:           training.ID.String(),
		CoachID:      training.CoachID.String(),
		Title:        training.Title,
		TrainingType: training.TrainingType,
		Goal:         training.Goal,
		Comment:      training.Comment,
		Items:        items,
		CreatedAt:    training.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:    training.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if training.Description.Valid {
		resp.Description = &training.Description.String
	}
	if resp.Items == nil {
		resp.Items = []TrainingItemResponse{}
	}
	return resp
}
