package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

type RepeaterHandler struct {
	queries *db.Queries
}

func NewRepeaterHandler(queries *db.Queries) *RepeaterHandler {
	return &RepeaterHandler{
		queries: queries,
	}
}

type CreateRepeaterRequest struct {
	Sets              int32    `json:"sets"`
	Reps              int32    `json:"reps"`
	Worktime          int32    `json:"worktime"`
	Resttime          int32    `json:"resttime"`
	SetRest           int32    `json:"set_rest"`
	TargetWeightRight *float32 `json:"target_weight_right,omitempty"`
	TargetWeightLeft  *float32 `json:"target_weight_left,omitempty"`
	SplitHand         bool     `json:"split_hand"`
	GripPosition      int32    `json:"grip_position"`
}

type UpdateRepeaterRequest struct {
	Sets              int32    `json:"sets"`
	Reps              int32    `json:"reps"`
	Worktime          int32    `json:"worktime"`
	Resttime          int32    `json:"resttime"`
	SetRest           int32    `json:"set_rest"`
	TargetWeightRight *float32 `json:"target_weight_right,omitempty"`
	TargetWeightLeft  *float32 `json:"target_weight_left,omitempty"`
	SplitHand         bool     `json:"split_hand"`
	GripPosition      int32    `json:"grip_position"`
}

type RepeaterResponse struct {
	ID                int32    `json:"id"`
	Sets              int32    `json:"sets"`
	Reps              int32    `json:"reps"`
	Worktime          int32    `json:"worktime"`
	Resttime          int32    `json:"resttime"`
	SetRest           int32    `json:"set_rest"`
	TargetWeightRight *float32 `json:"target_weight_right,omitempty"`
	TargetWeightLeft  *float32 `json:"target_weight_left,omitempty"`
	SplitHand         bool     `json:"split_hand"`
	GripPosition      int32    `json:"grip_position"`
}

// CreateRepeater godoc
// @Summary Create a new repeater configuration
// @Description Create a new repeater configuration with sets, reps, timings, and grip settings
// @Tags Repeater
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateRepeaterRequest true "Repeater configuration details"
// @Success 201 {object} RepeaterResponse "Repeater created successfully"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/repeaters [post]
func (h *RepeaterHandler) CreateRepeater(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	var req CreateRepeaterRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	var targetWeightRight, targetWeightLeft pgtype.Float4
	if req.TargetWeightRight != nil {
		targetWeightRight.Float32 = *req.TargetWeightRight
		targetWeightRight.Valid = true
	}
	if req.TargetWeightLeft != nil {
		targetWeightLeft.Float32 = *req.TargetWeightLeft
		targetWeightLeft.Valid = true
	}

	repeater, err := h.queries.CreateRepeater(context.Background(), db.CreateRepeaterParams{
		UserID:            userUUID,
		Sets:              req.Sets,
		Reps:              req.Reps,
		Worktime:          req.Worktime,
		Resttime:          req.Resttime,
		SetRest:           req.SetRest,
		TargetWeightRight: targetWeightRight,
		TargetWeightLeft:  targetWeightLeft,
		SplitHand:         req.SplitHand,
		GripPosition:      req.GripPosition,
	})
	if err != nil {
		slog.Error("failed to create repeater", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create repeater"})
	}

	return c.Status(fiber.StatusCreated).JSON(repeater)
}

// GetRepeaters godoc
// @Summary Get all repeater configurations
// @Description Retrieve all repeater configurations for the authenticated user
// @Tags Repeater
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} RepeaterResponse "List of repeaters"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/repeaters [get]
func (h *RepeaterHandler) GetRepeaters(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	repeaters, err := h.queries.GetUserRepeaters(context.Background(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve repeaters", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve repeaters"})
	}

	return c.JSON(repeaters)
}

// GetRepeater godoc
// @Summary Get a repeater by ID
// @Description Retrieve a specific repeater configuration by ID
// @Tags Repeater
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Repeater ID (UUID)"
// @Success 200 {object} RepeaterResponse "Repeater details"
// @Failure 400 {object} map[string]string "Invalid repeater ID"
// @Failure 404 {object} map[string]string "Repeater not found"
// @Router /api/repeaters/{id} [get]
func (h *RepeaterHandler) GetRepeater(c fiber.Ctx) error {
	idStr := c.Params("id")
	var repeaterUUID pgtype.UUID
	if err := repeaterUUID.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid repeater ID"})
	}

	repeater, err := h.queries.GetRepeater(context.Background(), repeaterUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Repeater not found"})
	}

	return c.JSON(repeater)
}

// UpdateRepeater godoc
// @Summary Update a repeater configuration
// @Description Update a repeater's configuration settings
// @Tags Repeater
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Repeater ID (UUID)"
// @Param request body UpdateRepeaterRequest true "Updated repeater configuration"
// @Success 200 {object} RepeaterResponse "Updated repeater"
// @Failure 400 {object} map[string]string "Invalid request or repeater ID"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/repeaters/{id} [put]
func (h *RepeaterHandler) UpdateRepeater(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	idStr := c.Params("id")
	var repeaterUUID pgtype.UUID
	if err := repeaterUUID.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid repeater ID"})
	}

	var req UpdateRepeaterRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	var targetWeightRight, targetWeightLeft pgtype.Float4
	if req.TargetWeightRight != nil {
		targetWeightRight.Float32 = *req.TargetWeightRight
		targetWeightRight.Valid = true
	}
	if req.TargetWeightLeft != nil {
		targetWeightLeft.Float32 = *req.TargetWeightLeft
		targetWeightLeft.Valid = true
	}

	updated, err := h.queries.UpdateRepeater(context.Background(), db.UpdateRepeaterParams{
		ID:                repeaterUUID,
		Sets:              req.Sets,
		Reps:              req.Reps,
		Worktime:          req.Worktime,
		Resttime:          req.Resttime,
		SetRest:           req.SetRest,
		TargetWeightRight: targetWeightRight,
		TargetWeightLeft:  targetWeightLeft,
		SplitHand:         req.SplitHand,
		GripPosition:      req.GripPosition,
	})
	if err != nil {
		slog.Error("failed to update repeater", "user_id", userID, "repeater_id", idStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update repeater"})
	}

	return c.JSON(updated)
}

// DeleteRepeater godoc
// @Summary Delete a repeater configuration
// @Description Delete a repeater configuration
// @Tags Repeater
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Repeater ID (UUID)"
// @Success 200 {object} map[string]string "Repeater deleted successfully"
// @Failure 400 {object} map[string]string "Invalid repeater ID"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/repeaters/{id} [delete]
func (h *RepeaterHandler) DeleteRepeater(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	idStr := c.Params("id")
	var repeaterUUID pgtype.UUID
	if err := repeaterUUID.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid repeater ID"})
	}

	if err := h.queries.DeleteRepeater(context.Background(), repeaterUUID); err != nil {
		slog.Error("failed to delete repeater", "user_id", userID, "repeater_id", idStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete repeater"})
	}

	return c.JSON(fiber.Map{"message": "Repeater deleted successfully"})
}
