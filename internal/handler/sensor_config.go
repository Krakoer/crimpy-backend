package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

type SensorConfigHandler struct {
	queries *db.Queries
}

func NewSensorConfigHandler(queries *db.Queries) *SensorConfigHandler {
	return &SensorConfigHandler{queries: queries}
}

type CreateSensorConfigRequest struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Index int64   `json:"index"`
	Tare  float32 `json:"tare"`
	Coef  float32 `json:"coef"`
}

type UpdateSensorConfigRequest struct {
	Name  string  `json:"name"`
	Index int64   `json:"index"`
	Tare  float32 `json:"tare"`
	Coef  float32 `json:"coef"`
}

// CreateSensorConfig godoc
// @Summary Create a sensor config
// @Description Create a new sensor calibration configuration for the authenticated user
// @Tags SensorConfig
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateSensorConfigRequest true "Sensor config details"
// @Success 201 {object} map[string]interface{} "Sensor config created"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sensor-configs [post]
func (h *SensorConfigHandler) CreateSensorConfig(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req CreateSensorConfigRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.ID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID is required"})
	}

	var id, userUUID pgtype.UUID
	if err := id.Scan(req.ID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid ID"})
	}
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	config, err := h.queries.CreateSensorConfig(context.Background(), db.CreateSensorConfigParams{
		ID:     id,
		UserID: userUUID,
		Name:   req.Name,
		Index:  req.Index,
		Tare:   req.Tare,
		Coef:   req.Coef,
	})
	if err != nil {
		slog.Error("failed to create sensor config", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create sensor config"})
	}

	return c.Status(fiber.StatusCreated).JSON(config)
}

// GetSensorConfigs godoc
// @Summary Get all sensor configs
// @Description Retrieve all sensor configs for the authenticated user
// @Tags SensorConfig
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} map[string]interface{} "List of sensor configs"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sensor-configs [get]
func (h *SensorConfigHandler) GetSensorConfigs(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	configs, err := h.queries.GetUserSensorConfigs(context.Background(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve sensor configs", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve sensor configs"})
	}

	return c.JSON(configs)
}

// UpdateSensorConfig godoc
// @Summary Update a sensor config
// @Description Update a sensor config. User must own it.
// @Tags SensorConfig
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Sensor config ID (UUID)"
// @Param request body UpdateSensorConfigRequest true "Updated sensor config"
// @Success 200 {object} map[string]interface{} "Updated sensor config"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sensor-configs/{id} [put]
func (h *SensorConfigHandler) UpdateSensorConfig(c fiber.Ctx) error {
	idStr := c.Params("id")
	var id pgtype.UUID
	if err := id.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid sensor config ID"})
	}

	existing, err := h.queries.GetSensorConfig(context.Background(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Sensor config not found"})
	}

	userID := middleware.GetUserID(c)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if existing.UserID.Bytes != userUUID.Bytes {
		slog.Warn("access denied to sensor config", "user_id", userID, "config_id", idStr)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	var req UpdateSensorConfigRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	updated, err := h.queries.UpdateSensorConfig(context.Background(), db.UpdateSensorConfigParams{
		ID:    id,
		Name:  req.Name,
		Index: req.Index,
		Tare:  req.Tare,
		Coef:  req.Coef,
	})
	if err != nil {
		slog.Error("failed to update sensor config", "user_id", userID, "config_id", idStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update sensor config"})
	}

	return c.JSON(updated)
}

// DeleteSensorConfig godoc
// @Summary Delete a sensor config
// @Description Delete a sensor config. User must own it.
// @Tags SensorConfig
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Sensor config ID (UUID)"
// @Success 200 {object} map[string]string "Deleted successfully"
// @Failure 400 {object} map[string]string "Invalid ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sensor-configs/{id} [delete]
func (h *SensorConfigHandler) DeleteSensorConfig(c fiber.Ctx) error {
	idStr := c.Params("id")
	var id pgtype.UUID
	if err := id.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid sensor config ID"})
	}

	existing, err := h.queries.GetSensorConfig(context.Background(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Sensor config not found"})
	}

	userID := middleware.GetUserID(c)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if existing.UserID.Bytes != userUUID.Bytes {
		slog.Warn("access denied to sensor config", "user_id", userID, "config_id", idStr)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteSensorConfig(context.Background(), id); err != nil {
		slog.Error("failed to delete sensor config", "user_id", userID, "config_id", idStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete sensor config"})
	}

	return c.JSON(fiber.Map{"message": "Sensor config deleted successfully"})
}
