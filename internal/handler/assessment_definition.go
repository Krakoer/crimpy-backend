package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"
	"strconv"
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

// assessmentPerTrainingConstraint is the unique index that keeps a training from
// being declared an assessment twice.
const assessmentPerTrainingConstraint = "assessment_definitions_training_id_idx"

// bodyweightRelativeUnitError is what a caller is told when they ask for a ratio
// of something that is not a weight, which the column's own check would refuse.
const bodyweightRelativeUnitError = "bodyweight_relative is only available on an assessment measured in kilograms"

// validBodyweightRelative reports whether the flag may be set for a unit. A
// score of (bodyweight + result) / bodyweight only reads as anything when the
// result is itself a weight.
func validBodyweightRelative(bodyweightRelative bool, unit string) bool {
	return !bodyweightRelative || unit == string(unitKilograms)
}

type CreateAssessmentDefinitionRequest struct {
	TrainingID string `json:"training_id"`
	Label      string `json:"label"`
	Prompt     string `json:"prompt"`
	Unit       string `json:"unit" enums:"kilograms,seconds,repetitions"`
	PerHand    bool   `json:"per_hand"`
	// Display the result as a ratio to the bodyweight it was pulled at, which
	// only a result in kilograms can be.
	BodyweightRelative bool `json:"bodyweight_relative"`
}

type UpdateAssessmentDefinitionRequest struct {
	Label   string `json:"label"`
	Prompt  string `json:"prompt"`
	Unit    string `json:"unit" enums:"kilograms,seconds,repetitions"`
	PerHand bool   `json:"per_hand"`
	// Free to toggle at any time, unlike the unit and the hands: see the freeze
	// rule in UpdateAssessmentDefinition.
	//
	// A pointer so that omitting it keeps what is stored. Every other field here
	// is either refused when empty or frozen by results, so this is the one an
	// older client could silently clear by sending the payload it has always
	// sent, taking the ratio off a whole history with a 200 and no warning.
	BodyweightRelative *bool `json:"bodyweight_relative"`
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
	// Whether the result reads as a ratio to the bodyweight it was pulled at,
	// (bodyweight + result) / bodyweight, rather than as an absolute load. A
	// display concern: the raw kilograms and the dated bodyweight are what is
	// stored, so the formula can be corrected without rewriting history.
	BodyweightRelative bool `json:"bodyweight_relative"`
	IsBuiltin          bool `json:"is_builtin"`
	// The program that reads the training backing this assessment, set only in
	// the recordable listing and only on a row the caller reaches through a
	// prescription. A coach's training is not readable on its own, so without
	// this id the assessment names a training the caller cannot run.
	ProgramID *string `json:"program_id,omitempty"`
	// Set once the unit and the hands can no longer move: results were measured
	// against them, or a training reads a number against them.
	UnitLocked bool   `json:"unit_locked"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func assessmentDefinitionToResponse(d db.AssessmentDefinition) AssessmentDefinitionResponse {
	resp := AssessmentDefinitionResponse{
		ID:                 d.ID.String(),
		Label:              d.Label,
		Unit:               d.Unit,
		PerHand:            d.PerHand,
		BodyweightRelative: d.BodyweightRelative,
		IsBuiltin:          !d.UserID.Valid,
		CreatedAt:          d.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:          d.UpdatedAt.Time.UTC().Format(time.RFC3339),
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
// @Description The assessments Crimpy ships plus the caller's own, builtins first. With recordable=true the set widens to the ones a result may be recorded against, which adds a coach's assessment whose training a program prescribed to the caller; each of those carries the program_id that reads the training, since a coach's training is only readable under a program.
// @Tags Assessments
// @Produce json
// @Security BearerAuth
// @Param recordable query bool false "Serve the assessments a result may be recorded against rather than the catalog the caller may reference"
// @Success 200 {array} AssessmentDefinitionResponse "Assessments"
// @Failure 400 {object} map[string]string "Invalid recordable"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Router /api/assessment-definitions [get]
func (h *AssessmentDefinitionHandler) GetAssessmentDefinitions(c fiber.Ctx) error {
	var userUUID pgtype.UUID
	if err := userUUID.Scan(middleware.GetUserID(c)); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	recordable := false
	if raw := c.Query("recordable"); raw != "" {
		wanted, err := strconv.ParseBool(raw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid recordable"})
		}
		recordable = wanted
	}

	var responses []AssessmentDefinitionResponse
	var err error
	if recordable {
		responses, err = h.recordableDefinitions(c, userUUID)
	} else {
		responses, err = h.referenceableDefinitions(c, userUUID)
	}
	if err != nil {
		slog.Error("failed to retrieve assessment definitions", "recordable", recordable, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve assessments"})
	}
	return c.Status(fiber.StatusOK).JSON(responses)
}

// referenceableDefinitions is the catalog: what the caller may name in a
// training of their own, so the builtins and their own writing.
func (h *AssessmentDefinitionHandler) referenceableDefinitions(c fiber.Ctx, userUUID pgtype.UUID) ([]AssessmentDefinitionResponse, error) {
	rows, err := h.queries.GetAssessmentDefinitions(c.Context(), userUUID)
	if err != nil {
		return nil, err
	}

	ids := make([]pgtype.UUID, 0, len(rows))
	responses := make([]AssessmentDefinitionResponse, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		responses = append(responses, assessmentDefinitionToResponse(row))
	}
	return h.withUnitLocks(c, responses, ids)
}

// recordableDefinitions is what a result may be recorded against, which is the
// catalog plus a coach's assessment the caller was prescribed. Served by the
// same query the record path checks a single assessment with, so what is listed
// here and what is accepted there cannot come apart.
func (h *AssessmentDefinitionHandler) recordableDefinitions(c fiber.Ctx, userUUID pgtype.UUID) ([]AssessmentDefinitionResponse, error) {
	rows, err := h.queries.GetRecordableAssessmentDefinitions(c.Context(), db.GetRecordableAssessmentDefinitionsParams{
		UserID: userUUID,
	})
	if err != nil {
		return nil, err
	}

	ids := make([]pgtype.UUID, 0, len(rows))
	responses := make([]AssessmentDefinitionResponse, 0, len(rows))
	for _, row := range rows {
		response := assessmentDefinitionToResponse(row.AssessmentDefinition)
		if row.ProgramID.Valid {
			programID := row.ProgramID.String()
			response.ProgramID = &programID
		}
		ids = append(ids, row.AssessmentDefinition.ID)
		responses = append(responses, response)
	}
	return h.withUnitLocks(c, responses, ids)
}

// withUnitLocks fills in UnitLocked, which is a per definition question the
// listing queries do not answer. Kept on both listings so one row never reads
// differently depending on which of them served it.
func (h *AssessmentDefinitionHandler) withUnitLocks(c fiber.Ctx, responses []AssessmentDefinitionResponse, ids []pgtype.UUID) ([]AssessmentDefinitionResponse, error) {
	locked, err := lockedAssessmentUnits(c.Context(), h.queries, ids)
	if err != nil {
		return nil, err
	}
	for i := range responses {
		responses[i].UnitLocked = locked[responses[i].ID]
	}
	return responses, nil
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
// @Failure 409 {object} map[string]string "Training is already an assessment"
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
	if !validBodyweightRelative(req.BodyweightRelative, req.Unit) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": bodyweightRelativeUnitError})
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
		UserID:             userUUID,
		TrainingID:         trainingUUID,
		Label:              req.Label,
		Prompt:             pgtype.Text{String: req.Prompt, Valid: true},
		Unit:               req.Unit,
		PerHand:            req.PerHand,
		BodyweightRelative: req.BodyweightRelative,
	})
	if err != nil {
		// One assessment per training, enforced by a unique index: a retry or a
		// double tap is a refusal, not a server fault.
		if constraint, isUnique := uniqueViolation(err); isUnique && constraint == assessmentPerTrainingConstraint {
			slog.Warn("training is already an assessment", "training_id", req.TrainingID)
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "This training is already an assessment",
			})
		}
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
// @Failure 409 {object} map[string]string "Unit or hands frozen by results or references"
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
	// An absent flag keeps what is stored, except when the unit is moving to one
	// that cannot carry a ratio: a client that does not know the field exists
	// cannot clear it, and the column would refuse the pair anyway, so refusing
	// the whole request would leave it with no way through.
	bodyweightRelative := definition.BodyweightRelative
	if req.BodyweightRelative != nil {
		bodyweightRelative = *req.BodyweightRelative
	} else if req.Unit != string(unitKilograms) {
		bodyweightRelative = false
	}
	if !validBodyweightRelative(bodyweightRelative, req.Unit) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": bodyweightRelativeUnitError})
	}

	// The bodyweight relative flag is deliberately absent from this condition,
	// and so from the CountAssessmentsForDefinition check below. What freezes
	// the unit and the hands is that past results were measured under them, and
	// moving them would restate what those stored numbers mean. This flag stores
	// nothing: the rows keep raw kilograms and the bodyweight series keeps the
	// denominator, so turning it on or off only changes how the same numbers are
	// drawn, for the whole history at once and reversibly. A coach who realises
	// mid season that a test reads better as a ratio may say so.
	if req.Unit != definition.Unit || req.PerHand != definition.PerHand {
		measured, err := h.queries.CountAssessmentsForDefinition(c.Context(), definitionUUID)
		if err != nil {
			slog.Error("failed to count assessment results", "assessment_id", definitionUUID.String(), "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update assessment"})
		}
		if measured > 0 {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "Cannot change the unit or the hands of an assessment that has results",
			})
		}
		// A target may only reference an assessment measured in the unit of the
		// field it drives, checked when the target is written. Moving the unit
		// afterwards would leave that reference standing but no longer resolving,
		// so the coach would read a live prescription that quietly uses its
		// fallback.
		referenced, err := h.queries.CountReferencesToAssessment(c.Context(), definitionUUID.String())
		if err != nil {
			slog.Error("failed to count assessment references", "assessment_id", definitionUUID.String(), "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update assessment"})
		}
		if referenced > 0 {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "Cannot change the unit or the hands of an assessment a training reads a number against",
			})
		}
	}

	updated, err := h.queries.UpdateAssessmentDefinition(c.Context(), db.UpdateAssessmentDefinitionParams{
		ID:                 definitionUUID,
		Label:              req.Label,
		Prompt:             pgtype.Text{String: req.Prompt, Valid: true},
		Unit:               req.Unit,
		PerHand:            req.PerHand,
		BodyweightRelative: bodyweightRelative,
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
// @Success 200 {object} map[string]string "Assessment deleted"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Assessment not found"
// @Failure 409 {object} map[string]string "Assessment has results or is referenced"
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
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "Cannot delete an assessment that has results",
		})
	}

	// A training reading a number against it would be left holding a reference
	// nothing answers, which refuses its next save rather than degrading.
	referenced, err := h.queries.CountReferencesToAssessment(c.Context(), definitionUUID.String())
	if err != nil {
		slog.Error("failed to count assessment references", "assessment_id", definitionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete assessment"})
	}
	if referenced > 0 {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "Cannot delete an assessment a training reads a number against",
		})
	}

	if err := h.queries.DeleteAssessmentDefinition(c.Context(), definitionUUID); err != nil {
		slog.Error("failed to delete assessment definition", "assessment_id", definitionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete assessment"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Assessment deleted successfully"})
}
