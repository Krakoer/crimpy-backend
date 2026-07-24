package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

type BuiltinTrainingWeightHandler struct {
	queries *db.Queries
}

func NewBuiltinTrainingWeightHandler(queries *db.Queries) *BuiltinTrainingWeightHandler {
	return &BuiltinTrainingWeightHandler{queries: queries}
}

type CreateBuiltinTrainingWeightRequest struct {
	ID                string  `json:"id"`
	BuiltinTrainingID string  `json:"builtin_training_id"`
	CustomWeightLeft  float32 `json:"custom_weight_left"`
	CustomWeightRight float32 `json:"custom_weight_right"`
}

type UpdateBuiltinTrainingWeightRequest struct {
	CustomWeightLeft  float32 `json:"custom_weight_left"`
	CustomWeightRight float32 `json:"custom_weight_right"`
}

// CreateBuiltinTrainingWeight godoc
// @Summary Create a builtin training weight override
// @Description Create a custom weight override for a builtin training
// @Tags BuiltinTrainingWeight
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateBuiltinTrainingWeightRequest true "Weight override details"
// @Success 201 {object} map[string]interface{} "Weight override created"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/builtin-training-weights [post]
func (h *BuiltinTrainingWeightHandler) CreateBuiltinTrainingWeight(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req CreateBuiltinTrainingWeightRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.ID == "" || req.BuiltinTrainingID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID and builtin_training_id are required"})
	}

	var id, userUUID, builtinID pgtype.UUID
	if err := id.Scan(req.ID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid ID"})
	}
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}
	if err := builtinID.Scan(req.BuiltinTrainingID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid builtin_training_id"})
	}

	weight, err := h.queries.CreateBuiltinTrainingWeight(c.Context(), db.CreateBuiltinTrainingWeightParams{
		ID:                id,
		UserID:            userUUID,
		BuiltinTrainingID: builtinID,
		CustomWeightLeft:  req.CustomWeightLeft,
		CustomWeightRight: req.CustomWeightRight,
	})
	if err != nil {
		slog.Error("failed to create builtin training weight", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create builtin training weight"})
	}

	return c.Status(fiber.StatusCreated).JSON(weight)
}

// GetBuiltinTrainingWeights godoc
// @Summary Get all builtin training weight overrides
// @Description Retrieve all custom weight overrides for the authenticated user
// @Tags BuiltinTrainingWeight
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} map[string]interface{} "List of weight overrides"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/builtin-training-weights [get]
func (h *BuiltinTrainingWeightHandler) GetBuiltinTrainingWeights(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	weights, err := h.queries.GetUserBuiltinTrainingWeights(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve builtin training weights", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve builtin training weights"})
	}

	if weights == nil {
		weights = []db.BuiltinTrainingWeight{}
	}
	return c.JSON(weights)
}

// UpdateBuiltinTrainingWeight godoc
// @Summary Update a builtin training weight override
// @Description Update custom weights. User must own it.
// @Tags BuiltinTrainingWeight
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Weight override ID (UUID)"
// @Param request body UpdateBuiltinTrainingWeightRequest true "Updated weights"
// @Success 200 {object} map[string]interface{} "Updated weight override"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/builtin-training-weights/{id} [put]
func (h *BuiltinTrainingWeightHandler) UpdateBuiltinTrainingWeight(c fiber.Ctx) error {
	idStr := c.Params("id")
	var id pgtype.UUID
	if err := id.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid ID"})
	}

	existing, err := h.queries.GetBuiltinTrainingWeight(c.Context(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Builtin training weight not found"})
	}

	userID := middleware.GetUserID(c)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if existing.UserID.Bytes != userUUID.Bytes {
		slog.Warn("access denied to builtin training weight", "user_id", userID, "weight_id", idStr)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	var req UpdateBuiltinTrainingWeightRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	updated, err := h.queries.UpdateBuiltinTrainingWeight(c.Context(), db.UpdateBuiltinTrainingWeightParams{
		ID:                id,
		CustomWeightLeft:  req.CustomWeightLeft,
		CustomWeightRight: req.CustomWeightRight,
	})
	if err != nil {
		slog.Error("failed to update builtin training weight", "user_id", userID, "weight_id", idStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update builtin training weight"})
	}

	return c.JSON(updated)
}

// DeleteBuiltinTrainingWeight godoc
// @Summary Delete a builtin training weight override
// @Description Delete a custom weight override. User must own it.
// @Tags BuiltinTrainingWeight
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Weight override ID (UUID)"
// @Success 200 {object} map[string]string "Deleted successfully"
// @Failure 400 {object} map[string]string "Invalid ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/builtin-training-weights/{id} [delete]
func (h *BuiltinTrainingWeightHandler) DeleteBuiltinTrainingWeight(c fiber.Ctx) error {
	idStr := c.Params("id")
	var id pgtype.UUID
	if err := id.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid ID"})
	}

	existing, err := h.queries.GetBuiltinTrainingWeight(c.Context(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Builtin training weight not found"})
	}

	userID := middleware.GetUserID(c)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if existing.UserID.Bytes != userUUID.Bytes {
		slog.Warn("access denied to builtin training weight", "user_id", userID, "weight_id", idStr)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteBuiltinTrainingWeight(c.Context(), id); err != nil {
		slog.Error("failed to delete builtin training weight", "user_id", userID, "weight_id", idStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete builtin training weight"})
	}

	return c.JSON(fiber.Map{"message": "Builtin training weight deleted successfully"})
}
