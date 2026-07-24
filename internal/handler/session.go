package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewSessionHandler(queries *db.Queries, pool *pgxpool.Pool) *SessionHandler {
	return &SessionHandler{
		queries: queries,
		pool:    pool,
	}
}

type CreateSessionRequest struct {
	Name              string              `json:"name"`
	Notes             string              `json:"notes"`
	Date              string              `json:"date,omitempty"`
	IsAssessment      bool                `json:"is_assessment"`
	SessionType       int32               `json:"session_type"`
	Duration          int32               `json:"duration"`
	RepeaterSets      *int32              `json:"repeater_sets,omitempty"`
	RepeaterReps      *int32              `json:"repeater_reps,omitempty"`
	RepeaterWorkTime  *int32              `json:"repeater_work_time,omitempty"`
	RepeaterRestTime  *int32              `json:"repeater_rest_time,omitempty"`
	RepeaterSetRest   *int32              `json:"repeater_set_rest,omitempty"`
	RepeaterSplitHand *bool               `json:"repeater_split_hand,omitempty"`
	RepDatas          []RepDataRequest    `json:"rep_datas,omitempty"`
	Assessments       []AssessmentRequest `json:"assessments,omitempty"`
}

type RepDataRequest struct {
	AverageWeight float32 `json:"average_weight"`
	IsRest        bool    `json:"is_rest"`
	RightHand     bool    `json:"right_hand"`
	Duration      int32   `json:"duration"`
	TargetWeight  float32 `json:"target_weight"`
	Index         int32   `json:"index"`
	GripPosition  int32   `json:"grip_position"`
}

type AssessmentRequest struct {
	Type         int32    `json:"type"`
	RightValue   *float32 `json:"right_value,omitempty"`
	LeftValue    *float32 `json:"left_value,omitempty"`
	GripPosition *int32   `json:"grip_position,omitempty"`
}

type UpdateSessionRequest struct {
	Name     string `json:"name"`
	Notes    string `json:"notes"`
	Duration int32  `json:"duration"`
}

type SessionResponse struct {
	ID                int32  `json:"id"`
	UserID            string `json:"user_id"`
	Name              string `json:"name"`
	Notes             string `json:"notes"`
	Date              string `json:"date"`
	IsAssessment      bool   `json:"is_assessment"`
	SessionType       int32  `json:"session_type"`
	Duration          int32  `json:"duration"`
	RepeaterSets      *int32 `json:"repeater_sets,omitempty"`
	RepeaterReps      *int32 `json:"repeater_reps,omitempty"`
	RepeaterWorkTime  *int32 `json:"repeater_work_time,omitempty"`
	RepeaterRestTime  *int32 `json:"repeater_rest_time,omitempty"`
	RepeaterSetRest   *int32 `json:"repeater_set_rest,omitempty"`
	RepeaterSplitHand *bool  `json:"repeater_split_hand,omitempty"`
}

// CreateSession godoc
// @Summary Create a new session
// @Description Create a new training session for the authenticated user with optional rep data and assessments
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateSessionRequest true "Session details"
// @Success 201 {object} SessionResponse "Session created successfully"
// @Failure 400 {object} map[string]string "Invalid request or validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sessions [post]
func (h *SessionHandler) CreateSession(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req CreateSessionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name is required"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	var sessionDate pgtype.Timestamptz
	if req.Date != "" {
		t, err := time.Parse(time.RFC3339Nano, req.Date)
		if err != nil {
			t, err = time.Parse(time.RFC3339, req.Date)
		}
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid date format"})
		}
		sessionDate = pgtype.Timestamptz{Time: t.UTC(), Valid: true}
	} else {
		sessionDate = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}

	var repeaterSets, repeaterReps, repeaterWorkTime, repeaterRestTime, repeaterSetRest pgtype.Int4
	var repeaterSplitHand pgtype.Bool

	if req.RepeaterSets != nil {
		repeaterSets.Int32 = *req.RepeaterSets
		repeaterSets.Valid = true
	}
	if req.RepeaterReps != nil {
		repeaterReps.Int32 = *req.RepeaterReps
		repeaterReps.Valid = true
	}
	if req.RepeaterWorkTime != nil {
		repeaterWorkTime.Int32 = *req.RepeaterWorkTime
		repeaterWorkTime.Valid = true
	}
	if req.RepeaterRestTime != nil {
		repeaterRestTime.Int32 = *req.RepeaterRestTime
		repeaterRestTime.Valid = true
	}
	if req.RepeaterSetRest != nil {
		repeaterSetRest.Int32 = *req.RepeaterSetRest
		repeaterSetRest.Valid = true
	}
	if req.RepeaterSplitHand != nil {
		repeaterSplitHand.Bool = *req.RepeaterSplitHand
		repeaterSplitHand.Valid = true
	}

	tx, err := h.pool.Begin(context.Background())
	if err != nil {
		slog.Error("failed to begin transaction", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
	}
	defer tx.Rollback(context.Background())

	qtx := h.queries.WithTx(tx)

	session, err := qtx.CreateSession(context.Background(), db.CreateSessionParams{
		UserID:            userUUID,
		Name:              req.Name,
		Notes:             req.Notes,
		Date:              sessionDate,
		IsAssessment:      req.IsAssessment,
		SessionType:       req.SessionType,
		Duration:          req.Duration,
		RepeaterSets:      repeaterSets,
		RepeaterReps:      repeaterReps,
		RepeaterWorkTime:  repeaterWorkTime,
		RepeaterRestTime:  repeaterRestTime,
		RepeaterSetRest:   repeaterSetRest,
		RepeaterSplitHand: repeaterSplitHand,
	})
	if err != nil {
		slog.Error("failed to create session", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
	}

	for _, rd := range req.RepDatas {
		_, err := qtx.CreateRepData(context.Background(), db.CreateRepDataParams{
			UserID:        userUUID,
			AverageWeight: rd.AverageWeight,
			SessionID:     session.ID,
			IsRest:        rd.IsRest,
			RightHand:     rd.RightHand,
			Duration:      rd.Duration,
			TargetWeight:  rd.TargetWeight,
			Index:         rd.Index,
			GripPosition:  rd.GripPosition,
		})
		if err != nil {
			slog.Error("failed to create rep data", "user_id", userID, "session_id", session.ID, "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create rep data"})
		}
	}

	for _, a := range req.Assessments {
		var rightValue, leftValue pgtype.Float4
		var gripPosition pgtype.Int4

		if a.RightValue != nil {
			rightValue.Float32 = *a.RightValue
			rightValue.Valid = true
		}
		if a.LeftValue != nil {
			leftValue.Float32 = *a.LeftValue
			leftValue.Valid = true
		}
		if a.GripPosition != nil {
			gripPosition.Int32 = *a.GripPosition
			gripPosition.Valid = true
		}

		_, err := qtx.CreateAssessment(context.Background(), db.CreateAssessmentParams{
			UserID:       userUUID,
			Type:         a.Type,
			RightValue:   rightValue,
			LeftValue:    leftValue,
			SessionID:    session.ID,
			GripPosition: gripPosition,
		})
		if err != nil {
			slog.Error("failed to create assessment", "user_id", userID, "session_id", session.ID, "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create assessment"})
		}
	}

	if err := tx.Commit(context.Background()); err != nil {
		slog.Error("failed to commit session transaction", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize session"})
	}

	return c.Status(fiber.StatusCreated).JSON(session)
}

// GetSessions godoc
// @Summary Get all sessions
// @Description Retrieve all training sessions for the authenticated user
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} SessionResponse "List of sessions"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sessions [get]
func (h *SessionHandler) GetSessions(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	sessions, err := h.queries.GetUserSessions(context.Background(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve sessions", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve sessions"})
	}

	if sessions == nil {
		sessions = []db.Session{}
	}
	return c.JSON(sessions)
}

// GetSession godoc
// @Summary Get a session by ID
// @Description Retrieve a specific session by ID with all related rep data and assessments. User must own the session unless they are an admin.
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Success 200 {object} map[string]interface{} "Session details with rep_datas and assessments"
// @Failure 400 {object} map[string]string "Invalid session ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Session not found"
// @Router /api/sessions/{id} [get]
func (h *SessionHandler) GetSession(c fiber.Ctx) error {
	idStr := c.Params("id")
	var sessionUUID pgtype.UUID
	if err := sessionUUID.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session ID"})
	}

	session, err := h.queries.GetSession(context.Background(), sessionUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
	}

	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if !isAdmin && session.UserID.Bytes != userUUID.Bytes {
		slog.Warn("access denied to session", "user_id", userID, "session_id", idStr)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	repDatas, _ := h.queries.GetSessionRepDatas(context.Background(), session.ID)
	assessments, _ := h.queries.GetSessionAssessments(context.Background(), session.ID)

	return c.JSON(fiber.Map{
		"session":     session,
		"rep_datas":   repDatas,
		"assessments": assessments,
	})
}

// UpdateSession godoc
// @Summary Update a session
// @Description Update a session's name, notes, and duration. User must own the session unless they are an admin.
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Param request body UpdateSessionRequest true "Updated session details"
// @Success 200 {object} SessionResponse "Updated session"
// @Failure 400 {object} map[string]string "Invalid request or session ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Session not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sessions/{id} [put]
func (h *SessionHandler) UpdateSession(c fiber.Ctx) error {
	idStr := c.Params("id")
	var sessionUUID pgtype.UUID
	if err := sessionUUID.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session ID"})
	}

	var req UpdateSessionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	session, err := h.queries.GetSession(context.Background(), sessionUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
	}

	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if !isAdmin && session.UserID.Bytes != userUUID.Bytes {
		slog.Warn("access denied to session", "user_id", userID, "session_id", idStr)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	updated, err := h.queries.UpdateSession(context.Background(), db.UpdateSessionParams{
		ID:       sessionUUID,
		Name:     req.Name,
		Notes:    req.Notes,
		Duration: req.Duration,
	})
	if err != nil {
		slog.Error("failed to update session", "user_id", userID, "session_id", idStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update session"})
	}

	return c.JSON(updated)
}

// DeleteSession godoc
// @Summary Delete a session
// @Description Delete a session. User must own the session unless they are an admin.
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Success 200 {object} map[string]string "Session deleted successfully"
// @Failure 400 {object} map[string]string "Invalid session ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Session not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sessions/{id} [delete]
func (h *SessionHandler) DeleteSession(c fiber.Ctx) error {
	idStr := c.Params("id")
	var sessionUUID pgtype.UUID
	if err := sessionUUID.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session ID"})
	}

	session, err := h.queries.GetSession(context.Background(), sessionUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
	}

	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if !isAdmin && session.UserID.Bytes != userUUID.Bytes {
		slog.Warn("access denied to session", "user_id", userID, "session_id", idStr)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteSession(context.Background(), sessionUUID); err != nil {
		slog.Error("failed to delete session", "user_id", userID, "session_id", idStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete session"})
	}

	return c.JSON(fiber.Map{"message": "Session deleted successfully"})
}
