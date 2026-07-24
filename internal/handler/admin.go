package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"

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
	EmailVerified  bool   `json:"email_verified"`
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
		slog.Warn("unauthorized admin access", "user_id", middleware.GetUserID(c), "path", c.Path())
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Admin access required",
		})
	}

	coaches, err := h.queries.GetPendingCoaches(c.Context())
	if err != nil {
		slog.Error("failed to retrieve pending coaches", "error", err)
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
			EmailVerified:  coach.EmailVerified,
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
		slog.Warn("unauthorized admin access", "user_id", middleware.GetUserID(c), "path", c.Path())
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

	coach, err := h.queries.GetUserByID(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve coach", "coach_id", coachID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve coach",
		})
	}

	if !coach.EmailVerified {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Coach must verify their email before admin validation",
		})
	}

	err = h.queries.ValidateCoach(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to validate coach", "coach_id", coachID, "error", err)
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
		slog.Warn("unauthorized admin access", "user_id", middleware.GetUserID(c), "path", c.Path())
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

	err := h.queries.RejectCoach(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to reject coach", "coach_id", coachID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to reject coach",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Coach rejected successfully",
	})
}

// ListUsers godoc
// @Summary List all users
// @Description Retrieve all users in the system (admin only)
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} UserResponse "List of all users"
// @Failure 403 {object} map[string]string "Admin access required"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/admin/users [get]
func (h *AdminHandler) ListUsers(c fiber.Ctx) error {
	if !middleware.IsAdmin(c) {
		slog.Warn("unauthorized admin access", "user_id", middleware.GetUserID(c), "path", c.Path())
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Admin access required",
		})
	}

	users, err := h.queries.ListAllUsers(c.Context())
	if err != nil {
		slog.Error("failed to retrieve users", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve users",
		})
	}

	var response []UserResponse
	for _, user := range users {
		response = append(response, UserResponse{
			ID:             user.ID.String(),
			Email:          user.Email,
			Firstname:      user.Firstname,
			Lastname:       user.Lastname,
			IsCoach:        user.IsCoach,
			CoachValidated: user.CoachValidated,
			IsAdmin:        user.IsAdmin,
			EmailVerified:  user.EmailVerified,
			CreatedAt:      user.CreatedAt.Time.String(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

// DeleteUser godoc
// @Summary Delete a user
// @Description Delete a user from the system (admin only)
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "User ID"
// @Success 200 {object} map[string]string "User deleted successfully"
// @Failure 400 {object} map[string]string "Invalid user ID or cannot delete admin"
// @Failure 403 {object} map[string]string "Admin access required"
// @Failure 404 {object} map[string]string "User not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/admin/users/{id} [delete]
func (h *AdminHandler) DeleteUser(c fiber.Ctx) error {
	if !middleware.IsAdmin(c) {
		slog.Warn("unauthorized admin access", "user_id", middleware.GetUserID(c), "path", c.Path())
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Admin access required",
		})
	}

	userID := c.Params("id")
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid user ID",
		})
	}

	user, err := h.queries.GetUserByID(c.Context(), userUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	if user.IsAdmin {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot delete admin users",
		})
	}

	err = h.queries.DeleteUser(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to delete user", "target_user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to delete user",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "User deleted successfully",
	})
}
