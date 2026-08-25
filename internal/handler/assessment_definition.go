package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AssessmentDefinitionHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewAssessmentDefinitionHandler(queries *db.Queries, pool *pgxpool.Pool) *AssessmentDefinitionHandler {
	return &AssessmentDefinitionHandler{queries: queries, pool: pool}
}

type CreateAssessmentDefinitionRequest struct {
	TrainingID string `json:"training_id"`
	Label      string `json:"label"`
	Prompt     string `json:"prompt"`
	Unit       string `json:"unit" enums:"kilograms,seconds,repetitions"`
	PerHand    bool   `json:"per_hand"`
}

type UpdateAssessmentDefinitionRequest struct {
	Label   string `json:"label"`
	Prompt  string `json:"prompt"`
	Unit    string `json:"unit" enums:"kilograms,seconds,repetitions"`
	PerHand bool   `json:"per_hand"`
}

type AssessmentDefinitionResponse struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Unit  string `json:"unit" enums:"kilograms,seconds,repetitions"`
	// Prompt and TrainingID are set on a coach written assessment and absent on
	// the ones Crimpy ships, which the app runs from a sensor protocol instead of
	// a training ending on a question.
	Prompt     *string `json:"prompt,omitempty"`
	TrainingID *string `json:"training_id,omitempty"`
	PerHand    bool    `json:"per_hand"`
	IsBuiltin  bool    `json:"is_builtin"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func assessmentDefinitionToResponse(d db.AssessmentDefinition) AssessmentDefinitionResponse {
	resp := AssessmentDefinitionResponse{
		ID:        d.ID.String(),
		Label:     d.Label,
		Unit:      d.Unit,
		PerHand:   d.PerHand,
		IsBuiltin: !d.UserID.Valid,
		CreatedAt: d.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt: d.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if d.Prompt.Valid {
		resp.Prompt = &d.Prompt.String
	}
	if d.TrainingID.Valid {
		trainingID := d.TrainingID.String()
		resp.TrainingID = &trainingID
	}
	return resp
}

func (h *AssessmentDefinitionHandler) ownedAssessmentDefinition() ownedResource[db.AssessmentDefinition] {
	return ownedResource[db.AssessmentDefinition]{
		label: "Assessment",
		fetch: h.queries.GetAssessmentDefinition,
		owner: func(d db.AssessmentDefinition) pgtype.UUID { return d.UserID },
	}
}

// GetAssessmentDefinitions godoc
// @Summary List the assessments the caller may reference
// @Description The assessments Crimpy ships plus the caller's own, builtins first.
// @Tags Assessments
// @Produce json
// @Security BearerAuth
// @Success 200 {array} AssessmentDefinitionResponse "Assessments"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Router /api/assessment-definitions [get]
func (h *AssessmentDefinitionHandler) GetAssessmentDefinitions(c fiber.Ctx) error {
	var userUUID pgtype.UUID
	if err := userUUID.Scan(middleware.GetUserID(c)); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	rows, err := h.queries.GetAssessmentDefinitions(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve assessment definitions", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve assessments"})
	}

	responses := make([]AssessmentDefinitionResponse, 0, len(rows))
	for _, row := range rows {
		responses = append(responses, assessmentDefinitionToResponse(row))
	}
	return c.Status(fiber.StatusOK).JSON(responses)
}

// CreateAssessmentDefinition godoc
// @Summary Create a custom assessment
// @Description Turn one of the caller's trainings into an assessment: it is run like any training and ends on the question the prompt asks.
// @Tags Assessments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateAssessmentDefinitionRequest true "Assessment details"
// @Success 201 {object} AssessmentDefinitionResponse "Created assessment"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Router /api/assessment-definitions [post]
func (h *AssessmentDefinitionHandler) CreateAssessmentDefinition(c fiber.Ctx) error {
	var userUUID pgtype.UUID
	if err := userUUID.Scan(middleware.GetUserID(c)); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	var req CreateAssessmentDefinitionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Label == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "label is required"})
	}
	if req.Prompt == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "prompt is required"})
	}
	if !validAssessmentUnits[assessmentUnit(req.Unit)] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid unit"})
	}

	var trainingUUID pgtype.UUID
	if err := trainingUUID.Scan(req.TrainingID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training_id"})
	}

	// The assessment is the training, so only its owner may declare it one.
	if _, err := h.queries.GetTrainingOwnedBy(c.Context(), db.GetTrainingOwnedByParams{
		ID:     trainingUUID,
		UserID: userUUID,
	}); err != nil {
		slog.Warn("assessment training not owned", "user_id", userUUID.String(), "training_id", req.TrainingID)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Unknown training_id"})
	}

	definition, err := h.queries.CreateAssessmentDefinition(c.Context(), db.CreateAssessmentDefinitionParams{
		UserID:     userUUID,
		TrainingID: trainingUUID,
		Label:      req.Label,
		Prompt:     pgtype.Text{String: req.Prompt, Valid: true},
		Unit:       req.Unit,
		PerHand:    req.PerHand,
	})
	if err != nil {
		slog.Error("failed to create assessment definition", "training_id", req.TrainingID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create assessment"})
	}

	return c.Status(fiber.StatusCreated).JSON(assessmentDefinitionToResponse(definition))
}

// UpdateAssessmentDefinition godoc
// @Summary Update a custom assessment
// @Description Rename an assessment or reword its question. The unit and the per hand flag are frozen once results have been measured, since changing them would restate what those numbers mean.
// @Tags Assessments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Assessment ID"
// @Param request body UpdateAssessmentDefinitionRequest true "Updated assessment"
// @Success 200 {object} AssessmentDefinitionResponse "Updated assessment"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Assessment not found"
// @Router /api/assessment-definitions/{id} [put]
func (h *AssessmentDefinitionHandler) UpdateAssessmentDefinition(c fiber.Ctx) error {
	definition, definitionUUID, ok := h.ownedAssessmentDefinition().require(c)
	if !ok {
		return nil
	}

	var req UpdateAssessmentDefinitionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Label == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "label is required"})
	}
	if req.Prompt == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "prompt is required"})
	}
	if !validAssessmentUnits[assessmentUnit(req.Unit)] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid unit"})
	}

	if req.Unit != definition.Unit || req.PerHand != definition.PerHand {
		measured, err := h.queries.CountAssessmentsForDefinition(c.Context(), definitionUUID)
		if err != nil {
			slog.Error("failed to count assessment results", "assessment_id", definitionUUID.String(), "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update assessment"})
		}
		if measured > 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Cannot change the unit or the hands of an assessment that has results",
			})
		}
	}

	updated, err := h.queries.UpdateAssessmentDefinition(c.Context(), db.UpdateAssessmentDefinitionParams{
		ID:      definitionUUID,
		Label:   req.Label,
		Prompt:  pgtype.Text{String: req.Prompt, Valid: true},
		Unit:    req.Unit,
		PerHand: req.PerHand,
	})
	if err != nil {
		slog.Error("failed to update assessment definition", "assessment_id", definitionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update assessment"})
	}

	return c.Status(fiber.StatusOK).JSON(assessmentDefinitionToResponse(updated))
}

// DeleteAssessmentDefinition godoc
// @Summary Delete a custom assessment
// @Description Refused while results have been measured against it, so a history can never be silently erased.
// @Tags Assessments
// @Produce json
// @Security BearerAuth
// @Param id path string true "Assessment ID"
// @Success 204 "Deleted"
// @Failure 400 {object} map[string]string "Assessment has results"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Assessment not found"
// @Router /api/assessment-definitions/{id} [delete]
func (h *AssessmentDefinitionHandler) DeleteAssessmentDefinition(c fiber.Ctx) error {
	_, definitionUUID, ok := h.ownedAssessmentDefinition().require(c)
	if !ok {
		return nil
	}

	measured, err := h.queries.CountAssessmentsForDefinition(c.Context(), definitionUUID)
	if err != nil {
		slog.Error("failed to count assessment results", "assessment_id", definitionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete assessment"})
	}
	if measured > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot delete an assessment that has results",
		})
	}

	if err := h.queries.DeleteAssessmentDefinition(c.Context(), definitionUUID); err != nil {
		slog.Error("failed to delete assessment definition", "assessment_id", definitionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete assessment"})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
