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

type CoachSessionHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewCoachSessionHandler(queries *db.Queries, pool *pgxpool.Pool) *CoachSessionHandler {
	return &CoachSessionHandler{queries: queries, pool: pool}
}

// SessionItemRequest represents one item in the session tree.
// Circuits and sections carry nested Items; exercises and hangboards are leaves.
type SessionItemRequest struct {
	Type              string               `json:"type"`
	Cycles            *int32               `json:"cycles"`
	CycleRestSeconds  *int32               `json:"cycle_rest_seconds"`
	Reps              *int32               `json:"reps"`
	Duration          *int32               `json:"duration"`
	RestSeconds       *int32               `json:"rest_seconds"`
	ExerciseID        *string              `json:"exercise_id"`
	HbWorktimeSeconds *int32               `json:"hb_worktime_seconds"`
	BothHands         *bool                `json:"both_hands"`
	Loads             json.RawMessage      `json:"loads"          swaggertype:"array,object"`
	HandPositions     json.RawMessage      `json:"hand_positions"  swaggertype:"array,object"`
	EdgeSizesMm       json.RawMessage      `json:"edge_sizes_mm"   swaggertype:"array,integer"`
	SectionTitle      *string              `json:"section_title"`
	Items             []SessionItemRequest `json:"items"`
}

type CreateCoachSessionRequest struct {
	Title       string               `json:"title"`
	Description *string              `json:"description"`
	Items       []SessionItemRequest `json:"items"`
}

type UpdateCoachSessionRequest struct {
	Title       string               `json:"title"`
	Description *string              `json:"description"`
	Items       []SessionItemRequest `json:"items"`
}

// SessionItemResponse mirrors SessionItemRequest with added server-assigned fields.
type SessionItemResponse struct {
	ID                string                `json:"id"`
	Type              string                `json:"type"`
	Position          int32                 `json:"position"`
	Cycles            *int32                `json:"cycles,omitempty"`
	CycleRestSeconds  *int32                `json:"cycle_rest_seconds,omitempty"`
	Reps              *int32                `json:"reps,omitempty"`
	Duration          *int32                `json:"duration,omitempty"`
	RestSeconds       *int32                `json:"rest_seconds,omitempty"`
	ExerciseID        *string               `json:"exercise_id,omitempty"`
	HbWorktimeSeconds *int32                `json:"hb_worktime_seconds,omitempty"`
	BothHands         *bool                 `json:"both_hands,omitempty"`
	Loads             json.RawMessage       `json:"loads,omitempty"          swaggertype:"array,object"`
	HandPositions     json.RawMessage       `json:"hand_positions,omitempty"  swaggertype:"array,object"`
	EdgeSizesMm       json.RawMessage       `json:"edge_sizes_mm,omitempty"   swaggertype:"array,integer"`
	SectionTitle      *string               `json:"section_title,omitempty"`
	Items             []SessionItemResponse `json:"items,omitempty"`
}

type CoachSessionResponse struct {
	ID          string                `json:"id"`
	CoachID     string                `json:"coach_id"`
	Title       string                `json:"title"`
	Description *string               `json:"description"`
	Items       []SessionItemResponse `json:"items"`
	CreatedAt   string                `json:"created_at"`
	UpdatedAt   string                `json:"updated_at"`
}

type CoachSessionListItem struct {
	ID          string  `json:"id"`
	CoachID     string  `json:"coach_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func coachSessionToListItem(s db.CoachSession) CoachSessionListItem {
	item := CoachSessionListItem{
		ID:        s.ID.String(),
		CoachID:   s.CoachID.String(),
		Title:     s.Title,
		CreatedAt: s.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if s.Description.Valid {
		item.Description = &s.Description.String
	}
	return item
}

// insertItemsRecursive inserts a tree of items into the DB, preserving order and parent links.
func insertItemsRecursive(
	ctx context.Context,
	queries *db.Queries,
	sessionID pgtype.UUID,
	parentID pgtype.UUID,
	items []SessionItemRequest,
) ([]SessionItemResponse, error) {
	result := make([]SessionItemResponse, 0, len(items))

	for i, req := range items {
		params := db.CreateCoachSessionItemParams{
			SessionID: sessionID,
			ParentID:  parentID,
			Type:      req.Type,
			Position:  int32(i),
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
		if req.HbWorktimeSeconds != nil {
			params.HbWorktimeSeconds = pgtype.Int4{Int32: *req.HbWorktimeSeconds, Valid: true}
		}
		if req.BothHands != nil {
			params.BothHands = pgtype.Bool{Bool: *req.BothHands, Valid: true}
		}
		if len(req.Loads) > 0 && string(req.Loads) != "null" {
			params.Loads = req.Loads
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

		row, err := queries.CreateCoachSessionItem(ctx, params)
		if err != nil {
			return nil, err
		}

		resp := dbItemToResponse(row)

		if len(req.Items) > 0 {
			children, err := insertItemsRecursive(ctx, queries, sessionID, row.ID, req.Items)
			if err != nil {
				return nil, err
			}
			resp.Items = children
		}

		result = append(result, resp)
	}

	return result, nil
}

func dbItemToResponse(r db.CoachSessionItem) SessionItemResponse {
	resp := SessionItemResponse{
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
	if r.HbWorktimeSeconds.Valid {
		resp.HbWorktimeSeconds = &r.HbWorktimeSeconds.Int32
	}
	if r.BothHands.Valid {
		resp.BothHands = &r.BothHands.Bool
	}
	if len(r.Loads) > 0 {
		resp.Loads = json.RawMessage(r.Loads)
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

// buildItemTree reconstructs the nested tree from a flat list of DB rows.
// Items are ordered by position and grouped by parent.
func buildItemTree(rows []db.CoachSessionItem, parentID pgtype.UUID) []SessionItemResponse {
	result := make([]SessionItemResponse, 0)
	for _, row := range rows {
		if row.ParentID.Bytes == parentID.Bytes && row.ParentID.Valid == parentID.Valid {
			resp := dbItemToResponse(row)
			resp.Items = buildItemTree(rows, row.ID)
			result = append(result, resp)
		}
	}
	return result
}

// CreateCoachSession godoc
// @Summary Create a coach session template
// @Description Create a new session template with a structured item tree. Requires a validated coach account.
// @Tags CoachSessions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateCoachSessionRequest true "Session data"
// @Success 201 {object} CoachSessionResponse "Session created"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/coach/sessions [post]
func (h *CoachSessionHandler) CreateCoachSession(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var req CreateCoachSessionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Title == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Title is required"})
	}

	tx, err := h.pool.Begin(context.Background())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
	}
	defer tx.Rollback(context.Background())

	qtx := h.queries.WithTx(tx)

	params := db.CreateCoachSessionParams{CoachID: coachUUID, Title: req.Title}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}

	session, err := qtx.CreateCoachSession(context.Background(), params)
	if err != nil {
		slog.Error("failed to create coach session", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
	}

	var zeroParent pgtype.UUID
	items, err := insertItemsRecursive(context.Background(), qtx, session.ID, zeroParent, req.Items)
	if err != nil {
		slog.Error("failed to insert session items", "session_id", session.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session items"})
	}

	if err := tx.Commit(context.Background()); err != nil {
		slog.Error("failed to commit session transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize session"})
	}

	return c.Status(fiber.StatusCreated).JSON(buildCoachSessionResponse(session, items))
}

// GetCoachSessions godoc
// @Summary List coach's session templates
// @Description Get all session templates for the authenticated coach (without items).
// @Tags CoachSessions
// @Produce json
// @Security BearerAuth
// @Success 200 {array} CoachSessionListItem "List of sessions"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Router /api/coach/sessions [get]
func (h *CoachSessionHandler) GetCoachSessions(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	sessions, err := h.queries.GetCoachSessions(context.Background(), coachUUID)
	if err != nil {
		slog.Error("failed to retrieve coach sessions", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve sessions"})
	}

	result := make([]CoachSessionListItem, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, coachSessionToListItem(s))
	}

	return c.Status(fiber.StatusOK).JSON(result)
}

// GetCoachSession godoc
// @Summary Get a coach session template
// @Description Get a session template with its full item tree. Only the owning coach can access.
// @Tags CoachSessions
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} CoachSessionResponse "Session with items"
// @Failure 400 {object} map[string]string "Invalid session ID"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Session not found"
// @Router /api/coach/sessions/{id} [get]
func (h *CoachSessionHandler) GetCoachSession(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var sessionUUID pgtype.UUID
	if err := sessionUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session ID"})
	}

	session, err := h.queries.GetCoachSession(context.Background(), sessionUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
		}
		slog.Error("failed to retrieve coach session", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve session"})
	}

	if session.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	rows, err := h.queries.GetCoachSessionItems(context.Background(), sessionUUID)
	if err != nil {
		slog.Error("failed to retrieve session items", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve session items"})
	}

	var zeroParent pgtype.UUID
	items := buildItemTree(rows, zeroParent)

	return c.Status(fiber.StatusOK).JSON(buildCoachSessionResponse(session, items))
}

// UpdateCoachSession godoc
// @Summary Update a coach session template
// @Description Replace the session metadata and items tree. Only the owning coach can update.
// @Tags CoachSessions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param request body UpdateCoachSessionRequest true "Updated session data"
// @Success 200 {object} CoachSessionResponse "Updated session"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Session not found"
// @Router /api/coach/sessions/{id} [put]
func (h *CoachSessionHandler) UpdateCoachSession(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var sessionUUID pgtype.UUID
	if err := sessionUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session ID"})
	}

	existing, err := h.queries.GetCoachSession(context.Background(), sessionUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
		}
		slog.Error("failed to retrieve coach session for update", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve session"})
	}

	if existing.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	var req UpdateCoachSessionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Title == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Title is required"})
	}

	tx, err := h.pool.Begin(context.Background())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update session"})
	}
	defer tx.Rollback(context.Background())

	qtx := h.queries.WithTx(tx)

	updateParams := db.UpdateCoachSessionParams{ID: sessionUUID, Title: req.Title}
	if req.Description != nil {
		updateParams.Description = pgtype.Text{String: *req.Description, Valid: true}
	}

	session, err := qtx.UpdateCoachSession(context.Background(), updateParams)
	if err != nil {
		slog.Error("failed to update coach session", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update session"})
	}

	if err := qtx.DeleteCoachSessionItems(context.Background(), sessionUUID); err != nil {
		slog.Error("failed to delete session items", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update session items"})
	}

	var zeroParent pgtype.UUID
	items, err := insertItemsRecursive(context.Background(), qtx, sessionUUID, zeroParent, req.Items)
	if err != nil {
		slog.Error("failed to insert session items", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update session items"})
	}

	if err := tx.Commit(context.Background()); err != nil {
		slog.Error("failed to commit session update", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize update"})
	}

	return c.Status(fiber.StatusOK).JSON(buildCoachSessionResponse(session, items))
}

// DeleteCoachSession godoc
// @Summary Delete a coach session template
// @Description Delete a session template and all its items. Only the owning coach can delete.
// @Tags CoachSessions
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} map[string]string "Session deleted"
// @Failure 400 {object} map[string]string "Invalid session ID"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Session not found"
// @Router /api/coach/sessions/{id} [delete]
func (h *CoachSessionHandler) DeleteCoachSession(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var sessionUUID pgtype.UUID
	if err := sessionUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session ID"})
	}

	session, err := h.queries.GetCoachSession(context.Background(), sessionUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
		}
		slog.Error("failed to retrieve coach session for delete", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve session"})
	}

	if session.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteCoachSession(context.Background(), sessionUUID); err != nil {
		slog.Error("failed to delete coach session", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete session"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Session deleted successfully"})
}

func buildCoachSessionResponse(session db.CoachSession, items []SessionItemResponse) CoachSessionResponse {
	resp := CoachSessionResponse{
		ID:        session.ID.String(),
		CoachID:   session.CoachID.String(),
		Title:     session.Title,
		Items:     items,
		CreatedAt: session.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt: session.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if session.Description.Valid {
		resp.Description = &session.Description.String
	}
	if resp.Items == nil {
		resp.Items = []SessionItemResponse{}
	}
	return resp
}
