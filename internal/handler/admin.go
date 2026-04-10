package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

type AdminHandler struct {
	queries *db.Queries
}

func NewAdminHandler(queries *db.Queries) *AdminHandler {
	return &AdminHandler{
		queries: queries,
	}
}

type CoachResponse struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Firstname      string `json:"firstname"`
	Lastname       string `json:"lastname"`
	IsCoach        bool   `json:"is_coach"`
	CoachValidated bool   `json:"coach_validated"`
	CreatedAt      string `json:"created_at"`
}

// GetPendingCoaches godoc
// @Summary Get pending coach accounts
// @Description Retrieve all coach accounts awaiting admin validation
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} CoachResponse "List of pending coaches"
// @Failure 403 {object} map[string]string "Admin access required"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/admin/coaches/pending [get]
func (h *AdminHandler) GetPendingCoaches(c fiber.Ctx) error {
	if !middleware.IsAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Admin access required",
		})
	}

	coaches, err := h.queries.GetPendingCoaches(context.Background())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve pending coaches",
		})
	}

	var response []CoachResponse
	for _, coach := range coaches {
		response = append(response, CoachResponse{
			ID:             coach.ID.String(),
			Email:          coach.Email,
			Firstname:      coach.Firstname,
			Lastname:       coach.Lastname,
			IsCoach:        coach.IsCoach,
			CoachValidated: coach.CoachValidated,
			CreatedAt:      coach.CreatedAt.Time.String(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

// ValidateCoach godoc
// @Summary Validate a coach account
// @Description Approve a pending coach account (admin only)
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Coach user ID"
// @Success 200 {object} map[string]string "Coach validated successfully"
// @Failure 400 {object} map[string]string "Invalid user ID"
// @Failure 403 {object} map[string]string "Admin access required"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/admin/coaches/{id}/validate [put]
func (h *AdminHandler) ValidateCoach(c fiber.Ctx) error {
	if !middleware.IsAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Admin access required",
		})
	}

	coachID := c.Params("id")
	var userUUID pgtype.UUID
	if err := userUUID.Scan(coachID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid user ID",
		})
	}

	err := h.queries.ValidateCoach(context.Background(), userUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to validate coach",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Coach validated successfully",
	})
}

// RejectCoach godoc
// @Summary Reject a coach account
// @Description Reject a pending coach account and revert to normal user (admin only)
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Coach user ID"
// @Success 200 {object} map[string]string "Coach rejected successfully"
// @Failure 400 {object} map[string]string "Invalid user ID"
// @Failure 403 {object} map[string]string "Admin access required"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/admin/coaches/{id}/reject [put]
func (h *AdminHandler) RejectCoach(c fiber.Ctx) error {
	if !middleware.IsAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Admin access required",
		})
	}

	coachID := c.Params("id")
	var userUUID pgtype.UUID
	if err := userUUID.Scan(coachID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid user ID",
		})
	}

	err := h.queries.RejectCoach(context.Background(), userUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to reject coach",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Coach rejected successfully",
	})
}
