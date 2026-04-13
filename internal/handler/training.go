package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

type TrainingHandler struct {
	queries *db.Queries
}

func NewTrainingHandler(queries *db.Queries) *TrainingHandler {
	return &TrainingHandler{
		queries: queries,
	}
}

type CreateTrainingRequest struct {
	Name         string               `json:"name"`
	RepeaterID   *string              `json:"repeater_id,omitempty"`
	IsFavorite   bool                 `json:"is_favorite"`
	IsAssessment bool                 `json:"is_assessment"`
	RepTemplates []RepTemplateRequest `json:"rep_templates,omitempty"`
}

type RepTemplateRequest struct {
	IsRest       bool    `json:"is_rest"`
	RightHand    bool    `json:"right_hand"`
	Duration     int32   `json:"duration"`
	TargetWeight float32 `json:"target_weight"`
	Index        int32   `json:"index"`
	GripPosition int32   `json:"grip_position"`
}

type UpdateTrainingRequest struct {
	Name       string `json:"name"`
	IsFavorite bool   `json:"is_favorite"`
}

type TrainingResponse struct {
	ID           int32  `json:"id"`
	UserID       string `json:"user_id"`
	Name         string `json:"name"`
	RepeaterID   *int32 `json:"repeater_id,omitempty"`
	IsFavorite   bool   `json:"is_favorite"`
	IsAssessment bool   `json:"is_assessment"`
}

// CreateTraining godoc
// @Summary Create a new training
// @Description Create a new training for the authenticated user with optional rep templates
// @Tags Training
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateTrainingRequest true "Training details"
// @Success 201 {object} TrainingResponse "Training created successfully"
// @Failure 400 {object} map[string]string "Invalid request or validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/trainings [post]
func (h *TrainingHandler) CreateTraining(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req CreateTrainingRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name is required"})
	}

	// Parse user UUID
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	// Create repeater ID type
	var repeaterID pgtype.UUID
	if req.RepeaterID != nil {
		if err := repeaterID.Scan(*req.RepeaterID); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid repeater ID"})
		}
	}

	training, err := h.queries.CreateTraining(context.Background(), db.CreateTrainingParams{
		UserID:       userUUID,
		Name:         req.Name,
		RepeaterID:   repeaterID,
		IsFavorite:   req.IsFavorite,
		IsAssessment: req.IsAssessment,
	})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create training"})
	}

	// Create rep templates if provided
	if len(req.RepTemplates) > 0 {
		for _, rt := range req.RepTemplates {
			_, err := h.queries.CreateRepTemplate(context.Background(), db.CreateRepTemplateParams{
				IsRest:       rt.IsRest,
				RightHand:    rt.RightHand,
				Duration:     rt.Duration,
				TrainingID:   training.ID,
				TargetWeight: rt.TargetWeight,
				Index:        rt.Index,
				GripPosition: rt.GripPosition,
			})
			if err != nil {
				// If rep template creation fails, we should delete the training
				_ = h.queries.DeleteTraining(context.Background(), training.ID)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create rep templates"})
			}
		}
	}

	return c.Status(fiber.StatusCreated).JSON(training)
}

// GetTrainings godoc
// @Summary Get all trainings
// @Description Retrieve all trainings for the authenticated user
// @Tags Training
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} TrainingResponse "List of trainings"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/trainings [get]
func (h *TrainingHandler) GetTrainings(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	trainings, err := h.queries.GetUserTrainings(context.Background(), userUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve trainings"})
	}

	return c.JSON(trainings)
}

// GetTraining godoc
// @Summary Get a training by ID
// @Description Retrieve a specific training by ID. User must own the training unless they are an admin.
// @Tags Training
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Training ID (UUID)"
// @Success 200 {object} TrainingResponse "Training details"
// @Failure 400 {object} map[string]string "Invalid training ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Training not found"
// @Router /api/trainings/{id} [get]
func (h *TrainingHandler) GetTraining(c fiber.Ctx) error {
	idStr := c.Params("id")
	var trainingUUID pgtype.UUID
	if err := trainingUUID.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}

	training, err := h.queries.GetTraining(context.Background(), trainingUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Training not found"})
	}

	// Verify the training belongs to the authenticated user (skip for admin)
	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if !isAdmin && training.UserID.Bytes != userUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	return c.JSON(training)
}

// UpdateTraining godoc
// @Summary Update a training
// @Description Update a training's name and favorite status. User must own the training unless they are an admin.
// @Tags Training
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Training ID (UUID)"
// @Param request body UpdateTrainingRequest true "Updated training details"
// @Success 200 {object} TrainingResponse "Updated training"
// @Failure 400 {object} map[string]string "Invalid request or training ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Training not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/trainings/{id} [put]
func (h *TrainingHandler) UpdateTraining(c fiber.Ctx) error {
	idStr := c.Params("id")
	var trainingUUID pgtype.UUID
	if err := trainingUUID.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}

	var req UpdateTrainingRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Verify ownership (skip for admin)
	training, err := h.queries.GetTraining(context.Background(), trainingUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Training not found"})
	}

	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if !isAdmin && training.UserID.Bytes != userUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	updated, err := h.queries.UpdateTraining(context.Background(), db.UpdateTrainingParams{
		ID:         trainingUUID,
		Name:       req.Name,
		IsFavorite: req.IsFavorite,
	})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training"})
	}

	return c.JSON(updated)
}

// DeleteTraining godoc
// @Summary Delete a training
// @Description Delete a training. User must own the training unless they are an admin.
// @Tags Training
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Training ID (UUID)"
// @Success 200 {object} map[string]string "Training deleted successfully"
// @Failure 400 {object} map[string]string "Invalid training ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Training not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/trainings/{id} [delete]
func (h *TrainingHandler) DeleteTraining(c fiber.Ctx) error {
	idStr := c.Params("id")
	var trainingUUID pgtype.UUID
	if err := trainingUUID.Scan(idStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}

	// Verify ownership (skip for admin)
	training, err := h.queries.GetTraining(context.Background(), trainingUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Training not found"})
	}

	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if !isAdmin && training.UserID.Bytes != userUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteTraining(context.Background(), trainingUUID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete training"})
	}

	return c.JSON(fiber.Map{"message": "Training deleted successfully"})
}
