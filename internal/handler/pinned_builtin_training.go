package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

type PinnedBuiltinTrainingHandler struct {
	queries *db.Queries
}

func NewPinnedBuiltinTrainingHandler(queries *db.Queries) *PinnedBuiltinTrainingHandler {
	return &PinnedBuiltinTrainingHandler{queries: queries}
}

type PinBuiltinTrainingRequest struct {
	BuiltinTrainingID string `json:"builtin_training_id"`
}

// PinBuiltinTraining godoc
// @Summary Pin a builtin training
// @Description Pin a builtin training for the authenticated user
// @Tags PinnedBuiltinTraining
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body PinBuiltinTrainingRequest true "Builtin training to pin"
// @Success 201 {object} map[string]interface{} "Pinned training"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/pinned-builtin-trainings [post]
func (h *PinnedBuiltinTrainingHandler) PinBuiltinTraining(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req PinBuiltinTrainingRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.BuiltinTrainingID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "builtin_training_id is required"})
	}

	var builtinID, userUUID pgtype.UUID
	if err := builtinID.Scan(req.BuiltinTrainingID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid builtin_training_id"})
	}
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	pinned, err := h.queries.PinBuiltinTraining(c.Context(), db.PinBuiltinTrainingParams{
		BuiltinTrainingID: builtinID,
		UserID:            userUUID,
	})
	if err != nil {
		slog.Error("failed to pin builtin training", "user_id", userID, "builtin_training_id", req.BuiltinTrainingID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to pin builtin training"})
	}

	return c.Status(fiber.StatusCreated).JSON(pinned)
}

// GetPinnedBuiltinTrainings godoc
// @Summary Get all pinned builtin trainings
// @Description Retrieve all pinned builtin trainings for the authenticated user
// @Tags PinnedBuiltinTraining
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} map[string]interface{} "List of pinned builtin trainings"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/pinned-builtin-trainings [get]
func (h *PinnedBuiltinTrainingHandler) GetPinnedBuiltinTrainings(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	pinned, err := h.queries.GetUserPinnedBuiltinTrainings(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve pinned builtin trainings", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve pinned builtin trainings"})
	}

	if pinned == nil {
		pinned = []db.PinnedBuiltinTraining{}
	}
	return c.JSON(pinned)
}

// UnpinBuiltinTraining godoc
// @Summary Unpin a builtin training
// @Description Remove a pinned builtin training for the authenticated user
// @Tags PinnedBuiltinTraining
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param builtin_training_id path string true "Builtin training ID (UUID)"
// @Success 200 {object} map[string]string "Unpinned successfully"
// @Failure 400 {object} map[string]string "Invalid ID"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/pinned-builtin-trainings/{builtin_training_id} [delete]
func (h *PinnedBuiltinTrainingHandler) UnpinBuiltinTraining(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	idStr := c.Params("builtin_training_id")
	var builtinID, userUUID pgtype.UUID
	if err := builtinID.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid builtin_training_id"})
	}
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if err := h.queries.UnpinBuiltinTraining(c.Context(), db.UnpinBuiltinTrainingParams{
		BuiltinTrainingID: builtinID,
		UserID:            userUUID,
	}); err != nil {
		slog.Error("failed to unpin builtin training", "user_id", userID, "builtin_training_id", idStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to unpin builtin training"})
	}

	return c.JSON(fiber.Map{"message": "Builtin training unpinned successfully"})
}
