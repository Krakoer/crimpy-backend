package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type BuiltinTrainingWeightHandler struct {
	queries *db.Queries
}

func NewBuiltinTrainingWeightHandler(queries *db.Queries) *BuiltinTrainingWeightHandler {
	return &BuiltinTrainingWeightHandler{queries: queries}
}

func (h *BuiltinTrainingWeightHandler) ownedWeight() ownedResource[db.BuiltinTrainingWeight] {
	return ownedResource[db.BuiltinTrainingWeight]{
		label: "Builtin training weight",
		fetch: h.queries.GetBuiltinTrainingWeight,
		owner: func(w db.BuiltinTrainingWeight) pgtype.UUID { return w.UserID },
	}
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

// BuiltinTrainingWeightResponse is the JSON a weight override is returned as. It
// is a mapped shape rather than the generated row, so reads speak the same
// snake_case the request bodies do.
type BuiltinTrainingWeightResponse struct {
	ID                string  `json:"id"`
	UserID            string  `json:"user_id"`
	BuiltinTrainingID string  `json:"builtin_training_id"`
	CustomWeightLeft  float32 `json:"custom_weight_left"`
	CustomWeightRight float32 `json:"custom_weight_right"`
	UpdatedAt         string  `json:"updated_at"`
}

func builtinTrainingWeightToResponse(w db.BuiltinTrainingWeight) BuiltinTrainingWeightResponse {
	return BuiltinTrainingWeightResponse{
		ID:                w.ID.String(),
		UserID:            w.UserID.String(),
		BuiltinTrainingID: w.BuiltinTrainingID.String(),
		CustomWeightLeft:  w.CustomWeightLeft,
		CustomWeightRight: w.CustomWeightRight,
		UpdatedAt:         w.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func builtinTrainingWeightsToResponses(rows []db.BuiltinTrainingWeight) []BuiltinTrainingWeightResponse {
	items := make([]BuiltinTrainingWeightResponse, 0, len(rows))
	for _, w := range rows {
		items = append(items, builtinTrainingWeightToResponse(w))
	}
	return items
}

// isUniqueViolation reports whether err is the database refusing a duplicate row.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// CreateBuiltinTrainingWeight godoc
// @Summary Create a builtin training weight override
// @Description Create a custom weight override for a builtin training
// @Tags BuiltinTrainingWeight
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateBuiltinTrainingWeightRequest true "Weight override details"
// @Success 201 {object} BuiltinTrainingWeightResponse "Weight override created"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 409 {object} map[string]string "Weight override already exists"
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
		if isUniqueViolation(err) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Weight override already exists for this builtin training"})
		}
		slog.Error("failed to create builtin training weight", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create builtin training weight"})
	}

	return c.Status(fiber.StatusCreated).JSON(builtinTrainingWeightToResponse(weight))
}

// GetBuiltinTrainingWeights godoc
// @Summary Get all builtin training weight overrides
// @Description Retrieve all custom weight overrides for the authenticated user
// @Tags BuiltinTrainingWeight
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} BuiltinTrainingWeightResponse "List of weight overrides"
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

	return c.JSON(builtinTrainingWeightsToResponses(weights))
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
// @Success 200 {object} BuiltinTrainingWeightResponse "Updated weight override"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/builtin-training-weights/{id} [put]
func (h *BuiltinTrainingWeightHandler) UpdateBuiltinTrainingWeight(c fiber.Ctx) error {
	_, id, ok := h.ownedWeight().require(c)
	if !ok {
		return nil
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
		slog.Error("failed to update builtin training weight", "weight_id", id.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update builtin training weight"})
	}

	return c.JSON(builtinTrainingWeightToResponse(updated))
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
	_, id, ok := h.ownedWeight().require(c)
	if !ok {
		return nil
	}

	if err := h.queries.DeleteBuiltinTrainingWeight(c.Context(), id); err != nil {
		slog.Error("failed to delete builtin training weight", "weight_id", id.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete builtin training weight"})
	}

	return c.JSON(fiber.Map{"message": "Builtin training weight deleted successfully"})
}
