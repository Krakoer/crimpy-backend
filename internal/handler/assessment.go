package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

type AssessmentHandler struct {
	queries *db.Queries
}

func NewAssessmentHandler(queries *db.Queries) *AssessmentHandler {
	return &AssessmentHandler{queries: queries}
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

	assessments, err := h.queries.GetUserAssessments(context.Background(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve assessments", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve assessments"})
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
	idStr := c.Params("id")
	var id pgtype.UUID
	if err := id.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid assessment ID"})
	}

	assessment, err := h.queries.GetAssessment(context.Background(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Assessment not found"})
	}

	userID := middleware.GetUserID(c)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if assessment.UserID.Bytes != userUUID.Bytes {
		slog.Warn("access denied to assessment", "user_id", userID, "assessment_id", idStr)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteAssessment(context.Background(), id); err != nil {
		slog.Error("failed to delete assessment", "user_id", userID, "assessment_id", idStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete assessment"})
	}

	return c.JSON(fiber.Map{"message": "Assessment deleted successfully"})
}
