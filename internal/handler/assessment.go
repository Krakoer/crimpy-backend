package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

type AssessmentHandler struct {
	queries *db.Queries
}

type CreateAssessmentRequest struct {
	SessionID    string   `json:"session_id"`
	Type         int32    `json:"type"`
	RightValue   *float32 `json:"right_value,omitempty"`
	LeftValue    *float32 `json:"left_value,omitempty"`
	GripPosition *int32   `json:"grip_position,omitempty"`
}

func NewAssessmentHandler(queries *db.Queries) *AssessmentHandler {
	return &AssessmentHandler{queries: queries}
}

func (h *AssessmentHandler) ownedAssessment() ownedResource[db.Assessment] {
	return ownedResource[db.Assessment]{
		label: "Assessment",
		fetch: h.queries.GetAssessment,
		owner: func(a db.Assessment) pgtype.UUID { return a.UserID },
	}
}

// CreateAssessment godoc
// @Summary Create an assessment linked to an existing session
// @Description Create a new assessment result for the authenticated user, linked to an existing session they own
// @Tags Assessment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateAssessmentRequest true "Assessment details"
// @Success 201 {object} map[string]interface{} "Assessment created"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Session not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/assessments [post]
func (h *AssessmentHandler) CreateAssessment(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req CreateAssessmentRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.SessionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "session_id is required"})
	}

	var sessionUUID pgtype.UUID
	if err := sessionUUID.Scan(req.SessionID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session_id"})
	}

	session, err := h.queries.GetSession(c.Context(), sessionUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if session.UserID.Bytes != userUUID.Bytes {
		slog.Warn("access denied to session for assessment", "user_id", userID, "session_id", req.SessionID)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	var rightValue, leftValue pgtype.Float4
	var gripPosition pgtype.Int4

	if req.RightValue != nil {
		rightValue.Float32 = *req.RightValue
		rightValue.Valid = true
	}
	if req.LeftValue != nil {
		leftValue.Float32 = *req.LeftValue
		leftValue.Valid = true
	}
	if req.GripPosition != nil {
		gripPosition.Int32 = *req.GripPosition
		gripPosition.Valid = true
	}

	assessment, err := h.queries.CreateAssessment(c.Context(), db.CreateAssessmentParams{
		UserID:       userUUID,
		Type:         req.Type,
		RightValue:   rightValue,
		LeftValue:    leftValue,
		SessionID:    session.ID,
		GripPosition: gripPosition,
	})
	if err != nil {
		slog.Error("failed to create assessment", "user_id", userID, "session_id", req.SessionID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create assessment"})
	}

	return c.Status(fiber.StatusCreated).JSON(assessment)
}

// GetAssessments godoc
// @Summary Get all assessments for the current user
// @Description Retrieve all assessment results for the authenticated user, ordered by session date descending
// @Tags Assessment
// @Produce json
// @Security BearerAuth
// @Success 200 {array} map[string]interface{} "List of assessments"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/assessments [get]
func (h *AssessmentHandler) GetAssessments(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	assessments, err := h.queries.GetUserAssessments(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve assessments", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve assessments"})
	}

	if assessments == nil {
		assessments = []db.GetUserAssessmentsRow{}
	}
	return c.JSON(assessments)
}

// DeleteAssessment godoc
// @Summary Delete an assessment
// @Description Delete an assessment. User must own the associated session.
// @Tags Assessment
// @Produce json
// @Security BearerAuth
// @Param id path string true "Assessment ID (UUID)"
// @Success 200 {object} map[string]string "Deleted successfully"
// @Failure 400 {object} map[string]string "Invalid ID"
// @Failure 404 {object} map[string]string "Not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/assessments/{id} [delete]
func (h *AssessmentHandler) DeleteAssessment(c fiber.Ctx) error {
	_, id, ok := h.ownedAssessment().require(c)
	if !ok {
		return nil
	}

	if err := h.queries.DeleteAssessment(c.Context(), id); err != nil {
		slog.Error("failed to delete assessment", "assessment_id", id.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete assessment"})
	}

	return c.JSON(fiber.Map{"message": "Assessment deleted successfully"})
}
