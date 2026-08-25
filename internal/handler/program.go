package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProgramHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewProgramHandler(queries *db.Queries, pool *pgxpool.Pool) *ProgramHandler {
	return &ProgramHandler{queries: queries, pool: pool}
}

type CreateProgramRequest struct {
	Name          string  `json:"name"`
	Objective     *string `json:"objective"`
	StartDate     string  `json:"start_date"`
	DurationWeeks *int32  `json:"duration_weeks"`
}

type UpdateProgramRequest = CreateProgramRequest

type ProgramResponse struct {
	ID            string  `json:"id"`
	CoachID       string  `json:"coach_id"`
	UserID        string  `json:"user_id"`
	Name          string  `json:"name"`
	Objective     *string `json:"objective,omitempty"`
	StartDate     string  `json:"start_date"`
	DurationWeeks *int32  `json:"duration_weeks,omitempty"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type ProgramListItem = ProgramResponse

func (h *ProgramHandler) verifyClientEnrolled(c fiber.Ctx, coachUUID pgtype.UUID, clientIDStr string) (pgtype.UUID, bool) {
	var clientUUID pgtype.UUID
	if err := clientUUID.Scan(clientIDStr); err != nil {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid client ID"})
		return pgtype.UUID{}, false
	}
	_, err := h.queries.GetCoachEnrollment(c.Context(), db.GetCoachEnrollmentParams{
		CoachID: coachUUID,
		UserID:  clientUUID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Client is not enrolled with this coach"})
		} else {
			slog.Error("failed to verify coach-client relationship", "error", err)
			c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal server error"})
		}
		return pgtype.UUID{}, false
	}
	return clientUUID, true
}

func (h *ProgramHandler) verifyProgramOwnership(c fiber.Ctx, coachUUID, clientUUID pgtype.UUID, programIDStr string) (pgtype.UUID, bool) {
	var programUUID pgtype.UUID
	if err := programUUID.Scan(programIDStr); err != nil {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid program ID"})
		return pgtype.UUID{}, false
	}
	program, err := h.queries.GetCoachProgram(c.Context(), programUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Program not found"})
		} else {
			slog.Error("failed to retrieve program", "program_id", programUUID.String(), "error", err)
			c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve program"})
		}
		return pgtype.UUID{}, false
	}
	if program.CoachID.Bytes != coachUUID.Bytes || program.UserID.Bytes != clientUUID.Bytes {
		c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
		return pgtype.UUID{}, false
	}
	return programUUID, true
}

func programToResponse(p db.CoachProgram) ProgramResponse {
	resp := ProgramResponse{
		ID:        p.ID.String(),
		CoachID:   p.CoachID.String(),
		UserID:    p.UserID.String(),
		Name:      p.Name,
		StartDate: p.StartDate.Time.Format(time.DateOnly),
		CreatedAt: p.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if p.Objective.Valid {
		resp.Objective = &p.Objective.String
	}
	if p.DurationWeeks.Valid {
		v := p.DurationWeeks.Int32
		resp.DurationWeeks = &v
	}
	return resp
}

func parseAndValidateProgramRequest(req CreateProgramRequest) (pgtype.Date, error) {
	if req.Name == "" {
		return pgtype.Date{}, errors.New("name is required")
	}
	t, err := time.Parse(time.DateOnly, req.StartDate)
	if err != nil {
		return pgtype.Date{}, errors.New("start_date must be in YYYY-MM-DD format")
	}
	// Program weeks run Monday to Sunday, so a program must start on a Monday.
	if t.Weekday() != time.Monday {
		return pgtype.Date{}, errors.New("start_date must be a Monday")
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

// CreateProgram godoc
// @Summary Create a training program for a client
// @Description Create a program for an enrolled client. Requires a validated coach account.
// @Tags Programs
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param request body CreateProgramRequest true "Program data"
// @Success 201 {object} ProgramResponse "Program created"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Not a validated coach or client not enrolled"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/coach/clients/{user_id}/programs [post]
func (h *ProgramHandler) CreateProgram(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}
	clientUUID, ok := h.verifyClientEnrolled(c, coachUUID, c.Params("user_id"))
	if !ok {
		return nil
	}

	var req CreateProgramRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	startDate, err := parseAndValidateProgramRequest(req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	params := db.CreateCoachProgramParams{
		CoachID:   coachUUID,
		UserID:    clientUUID,
		Name:      req.Name,
		StartDate: startDate,
	}
	if req.Objective != nil {
		params.Objective = pgtype.Text{String: *req.Objective, Valid: true}
	}
	if req.DurationWeeks != nil {
		params.DurationWeeks = pgtype.Int4{Int32: *req.DurationWeeks, Valid: true}
	}

	program, err := h.queries.CreateCoachProgram(c.Context(), params)
	if err != nil {
		slog.Error("failed to create program", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create program"})
	}

	return c.Status(fiber.StatusCreated).JSON(programToResponse(program))
}

// GetPrograms godoc
// @Summary List programs for a client
// @Description Get all training programs created by this coach for a specific client.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Success 200 {array} ProgramListItem "List of programs"
// @Failure 403 {object} map[string]string "Not a validated coach or client not enrolled"
// @Router /api/coach/clients/{user_id}/programs [get]
func (h *ProgramHandler) GetPrograms(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}
	clientUUID, ok := h.verifyClientEnrolled(c, coachUUID, c.Params("user_id"))
	if !ok {
		return nil
	}

	programs, err := h.queries.GetCoachProgramsForClient(c.Context(), db.GetCoachProgramsForClientParams{
		CoachID: coachUUID,
		UserID:  clientUUID,
	})
	if err != nil {
		slog.Error("failed to retrieve programs", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve programs"})
	}

	result := make([]ProgramResponse, 0, len(programs))
	for _, p := range programs {
		result = append(result, programToResponse(p))
	}
	return c.Status(fiber.StatusOK).JSON(result)
}

// GetProgram godoc
// @Summary Get a training program
// @Description Get a specific program by ID.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param program_id path string true "Program ID"
// @Success 200 {object} ProgramResponse "Program"
// @Failure 400 {object} map[string]string "Invalid ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Program not found"
// @Router /api/coach/clients/{user_id}/programs/{program_id} [get]
func (h *ProgramHandler) GetProgram(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}
	clientUUID, ok := h.verifyClientEnrolled(c, coachUUID, c.Params("user_id"))
	if !ok {
		return nil
	}
	programUUID, ok := h.verifyProgramOwnership(c, coachUUID, clientUUID, c.Params("program_id"))
	if !ok {
		return nil
	}

	program, err := h.queries.GetCoachProgram(c.Context(), programUUID)
	if err != nil {
		slog.Error("failed to retrieve program", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve program"})
	}

	return c.Status(fiber.StatusOK).JSON(programToResponse(program))
}

// UpdateProgram godoc
// @Summary Update a training program
// @Description Replace program metadata. Only the owning coach can update.
// @Tags Programs
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param program_id path string true "Program ID"
// @Param request body UpdateProgramRequest true "Updated program data"
// @Success 200 {object} ProgramResponse "Updated program"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Program not found"
// @Router /api/coach/clients/{user_id}/programs/{program_id} [put]
func (h *ProgramHandler) UpdateProgram(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}
	clientUUID, ok := h.verifyClientEnrolled(c, coachUUID, c.Params("user_id"))
	if !ok {
		return nil
	}
	programUUID, ok := h.verifyProgramOwnership(c, coachUUID, clientUUID, c.Params("program_id"))
	if !ok {
		return nil
	}

	var req UpdateProgramRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	startDate, err := parseAndValidateProgramRequest(req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	updateParams := db.UpdateCoachProgramParams{
		ID:        programUUID,
		Name:      req.Name,
		StartDate: startDate,
	}
	if req.Objective != nil {
		updateParams.Objective = pgtype.Text{String: *req.Objective, Valid: true}
	}
	if req.DurationWeeks != nil {
		updateParams.DurationWeeks = pgtype.Int4{Int32: *req.DurationWeeks, Valid: true}
	}

	program, err := h.queries.UpdateCoachProgram(c.Context(), updateParams)
	if err != nil {
		slog.Error("failed to update program", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update program"})
	}

	return c.Status(fiber.StatusOK).JSON(programToResponse(program))
}

// DeleteProgram godoc
// @Summary Delete a training program
// @Description Delete a program and its weeks. Only the owning coach can delete. Refused when any week holds a session the athlete already played, because the cascade would null the link that session keeps to what was prescribed.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param program_id path string true "Program ID"
// @Success 200 {object} map[string]string "Program deleted"
// @Failure 400 {object} map[string]string "Invalid ID, or the program holds a played session"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Program not found"
// @Router /api/coach/clients/{user_id}/programs/{program_id} [delete]
func (h *ProgramHandler) DeleteProgram(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}
	clientUUID, ok := h.verifyClientEnrolled(c, coachUUID, c.Params("user_id"))
	if !ok {
		return nil
	}
	programUUID, ok := h.verifyProgramOwnership(c, coachUUID, clientUUID, c.Params("program_id"))
	if !ok {
		return nil
	}

	// Cascades all the way down to the week sessions, so it is the week delete
	// refusal one level up.
	played, err := h.queries.CountPlayedSessionsInProgram(c.Context(), programUUID)
	if err != nil {
		slog.Error("failed to count played sessions", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete program"})
	}
	if played > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "This program holds a session the athlete already played and cannot be deleted",
		})
	}

	if err := h.queries.DeleteCoachProgram(c.Context(), programUUID); err != nil {
		slog.Error("failed to delete program", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete program"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Program deleted successfully"})
}

// GetMyPrograms godoc
// @Summary List my training programs
// @Description Get all programs assigned to the authenticated user by their coach.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Success 200 {array} ProgramListItem "List of programs"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/user/programs [get]
func (h *ProgramHandler) GetMyPrograms(c fiber.Ctx) error {
	userIDStr := middleware.GetUserID(c)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userIDStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	programs, err := h.queries.GetMyPrograms(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve user programs", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve programs"})
	}

	result := make([]ProgramResponse, 0, len(programs))
	for _, p := range programs {
		result = append(result, programToResponse(p))
	}
	return c.Status(fiber.StatusOK).JSON(result)
}

// GetMyProgram godoc
// @Summary Get one of my training programs
// @Description Get a specific program assigned to the authenticated user.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param program_id path string true "Program ID"
// @Success 200 {object} ProgramResponse "Program"
// @Failure 400 {object} map[string]string "Invalid ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Program not found"
// @Router /api/user/programs/{program_id} [get]
func (h *ProgramHandler) GetMyProgram(c fiber.Ctx) error {
	userIDStr := middleware.GetUserID(c)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userIDStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	var programUUID pgtype.UUID
	if err := programUUID.Scan(c.Params("program_id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid program ID"})
	}

	program, err := h.queries.GetCoachProgram(c.Context(), programUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Program not found"})
		}
		slog.Error("failed to retrieve program", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve program"})
	}

	if program.UserID.Bytes != userUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	return c.Status(fiber.StatusOK).JSON(programToResponse(program))
}

// GetMyProgramTraining godoc
// @Summary Get a training referenced by one of my programs
// @Description Get the full training tree for a training scheduled in a program assigned to the authenticated user. Authorized through program ownership rather than training ownership.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param program_id path string true "Program ID"
// @Param training_id path string true "Training ID"
// @Success 200 {object} TrainingResponse "Training"
// @Failure 400 {object} map[string]string "Invalid ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Training not found in program"
// @Router /api/user/programs/{program_id}/trainings/{training_id} [get]
func (h *ProgramHandler) GetMyProgramTraining(c fiber.Ctx) error {
	userIDStr := middleware.GetUserID(c)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userIDStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	var programUUID pgtype.UUID
	if err := programUUID.Scan(c.Params("program_id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid program ID"})
	}

	var trainingUUID pgtype.UUID
	if err := trainingUUID.Scan(c.Params("training_id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}

	count, err := h.queries.CountMyProgramTraining(c.Context(), db.CountMyProgramTrainingParams{
		ProgramID:  programUUID,
		UserID:     userUUID,
		TrainingID: trainingUUID,
	})
	if err != nil {
		slog.Error("failed to authorize program training", "program_id", programUUID.String(), "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training"})
	}
	if count == 0 {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	training, err := h.queries.GetTraining(c.Context(), trainingUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Training not found"})
		}
		slog.Error("failed to retrieve training", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training"})
	}

	rows, err := h.queries.GetTrainingItems(c.Context(), trainingUUID)
	if err != nil {
		slog.Error("failed to retrieve training items", "training_id", trainingUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training items"})
	}

	var zeroParent pgtype.UUID
	items := buildTrainingItemTree(rows, zeroParent)

	detail, err := buildTrainingDetail(c.Context(), h.queries, training, items)
	if err != nil {
		slog.Error("failed to resolve training assessments", "training_id", training.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training"})
	}

	return c.Status(fiber.StatusOK).JSON(detail)
}
