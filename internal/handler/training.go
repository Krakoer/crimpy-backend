package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"strconv"

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
	RepeaterID   *int32               `json:"repeater_id,omitempty"`
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

// CreateTraining creates a new training for the authenticated user
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
	var repeaterID pgtype.Int4
	if req.RepeaterID != nil {
		repeaterID.Int32 = *req.RepeaterID
		repeaterID.Valid = true
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

// GetTrainings retrieves all trainings for the authenticated user
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

// GetTraining retrieves a specific training by ID
func (h *TrainingHandler) GetTraining(c fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}

	training, err := h.queries.GetTraining(context.Background(), int32(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Training not found"})
	}

	// Verify the training belongs to the authenticated user (skip for admin)
	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)
	if !isAdmin && training.UserID.String() != userID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	return c.JSON(training)
}

// UpdateTraining updates a training
func (h *TrainingHandler) UpdateTraining(c fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}

	var req UpdateTrainingRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Verify ownership (skip for admin)
	training, err := h.queries.GetTraining(context.Background(), int32(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Training not found"})
	}

	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)
	if !isAdmin && training.UserID.String() != userID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	updated, err := h.queries.UpdateTraining(context.Background(), db.UpdateTrainingParams{
		ID:         int32(id),
		Name:       req.Name,
		IsFavorite: req.IsFavorite,
	})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update training"})
	}

	return c.JSON(updated)
}

// DeleteTraining deletes a training
func (h *TrainingHandler) DeleteTraining(c fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}

	// Verify ownership (skip for admin)
	training, err := h.queries.GetTraining(context.Background(), int32(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Training not found"})
	}

	userID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)
	if !isAdmin && training.UserID.String() != userID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteTraining(context.Background(), int32(id)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete training"})
	}

	return c.JSON(fiber.Map{"message": "Training deleted successfully"})
}
