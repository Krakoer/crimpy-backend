package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

type SessionHandler struct {
	queries *db.Queries
}

func NewSessionHandler(queries *db.Queries) *SessionHandler {
	return &SessionHandler{
		queries: queries,
	}
}

type CreateSessionRequest struct {
	Name              string              `json:"name"`
	Notes             string              `json:"notes"`
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

// CreateSession creates a new session for the authenticated user
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

	// Prepare optional fields
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

	session, err := h.queries.CreateSession(context.Background(), db.CreateSessionParams{
		UserID:            userUUID,
		Name:              req.Name,
		Notes:             req.Notes,
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
	}

	// Create rep datas if provided
	for _, rd := range req.RepDatas {
		_, err := h.queries.CreateRepData(context.Background(), db.CreateRepDataParams{
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
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create rep data"})
		}
	}

	// Create assessments if provided
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

		_, err := h.queries.CreateAssessment(context.Background(), db.CreateAssessmentParams{
			Type:         a.Type,
			RightValue:   rightValue,
			LeftValue:    leftValue,
			SessionID:    session.ID,
			GripPosition: gripPosition,
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create assessment"})
		}
	}

	return c.Status(fiber.StatusCreated).JSON(session)
}

// GetSessions retrieves all sessions for the authenticated user
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve sessions"})
	}

	return c.JSON(sessions)
}

// GetSession retrieves a specific session by ID with all related data
func (h *SessionHandler) GetSession(c fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session ID"})
	}

	session, err := h.queries.GetSession(context.Background(), int32(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
	}

	// Verify ownership (skip for admin)
	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)
	if !isAdmin && session.UserID.String() != userID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	// Get related data
	repDatas, _ := h.queries.GetSessionRepDatas(context.Background(), session.ID)
	assessments, _ := h.queries.GetSessionAssessments(context.Background(), session.ID)

	return c.JSON(fiber.Map{
		"session":     session,
		"rep_datas":   repDatas,
		"assessments": assessments,
	})
}

// UpdateSession updates a session
func (h *SessionHandler) UpdateSession(c fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session ID"})
	}

	var req UpdateSessionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Verify ownership (skip for admin)
	session, err := h.queries.GetSession(context.Background(), int32(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
	}

	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)
	if !isAdmin && session.UserID.String() != userID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	updated, err := h.queries.UpdateSession(context.Background(), db.UpdateSessionParams{
		ID:       int32(id),
		Name:     req.Name,
		Notes:    req.Notes,
		Duration: req.Duration,
	})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update session"})
	}

	return c.JSON(updated)
}

// DeleteSession deletes a session
func (h *SessionHandler) DeleteSession(c fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session ID"})
	}

	// Verify ownership (skip for admin)
	session, err := h.queries.GetSession(context.Background(), int32(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
	}

	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)
	if !isAdmin && session.UserID.String() != userID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteSession(context.Background(), int32(id)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete session"})
	}

	return c.JSON(fiber.Map{"message": "Session deleted successfully"})
}
