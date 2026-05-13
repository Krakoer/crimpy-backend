package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"errors"
	"fmt"
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

type ProgramSlotRequest struct {
	TrainingID   string `json:"training_id"`
	DayOfWeek    *int32 `json:"day_of_week"`
	TimesPerWeek *int32 `json:"times_per_week"`
}

type CreateProgramRequest struct {
	Name          string               `json:"name"`
	Objective     *string              `json:"objective"`
	StartDate     string               `json:"start_date"`
	DurationWeeks *int32               `json:"duration_weeks"`
	Slots         []ProgramSlotRequest `json:"slots"`
}

type UpdateProgramRequest = CreateProgramRequest

type ProgramSlotResponse struct {
	ID            string `json:"id"`
	TrainingID    string `json:"training_id"`
	TrainingTitle string `json:"training_title"`
	TrainingType  string `json:"training_type"`
	DayOfWeek     *int32 `json:"day_of_week,omitempty"`
	TimesPerWeek  *int32 `json:"times_per_week,omitempty"`
	Position      int32  `json:"position"`
}

type ProgramResponse struct {
	ID            string                `json:"id"`
	CoachID       string                `json:"coach_id"`
	UserID        string                `json:"user_id"`
	Name          string                `json:"name"`
	Objective     *string               `json:"objective,omitempty"`
	StartDate     string                `json:"start_date"`
	DurationWeeks *int32                `json:"duration_weeks,omitempty"`
	Slots         []ProgramSlotResponse `json:"slots"`
	CreatedAt     string                `json:"created_at"`
	UpdatedAt     string                `json:"updated_at"`
}

type ProgramListItem struct {
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

func validateSlot(slot ProgramSlotRequest) error {
	if slot.DayOfWeek != nil && slot.TimesPerWeek != nil {
		return errors.New("slot must have either day_of_week or times_per_week, not both")
	}
	if slot.DayOfWeek == nil && slot.TimesPerWeek == nil {
		return errors.New("slot must have either day_of_week or times_per_week")
	}
	if slot.DayOfWeek != nil && (*slot.DayOfWeek < 0 || *slot.DayOfWeek > 6) {
		return errors.New("day_of_week must be between 0 (Monday) and 6 (Sunday)")
	}
	if slot.TimesPerWeek != nil && *slot.TimesPerWeek <= 0 {
		return errors.New("times_per_week must be greater than 0")
	}
	return nil
}

func buildSlotsFromRows(rows []db.GetCoachProgramSlotsRow) []ProgramSlotResponse {
	result := make([]ProgramSlotResponse, 0, len(rows))
	for _, r := range rows {
		s := ProgramSlotResponse{
			ID:            r.ID.String(),
			TrainingID:    r.TrainingID.String(),
			TrainingTitle: r.TrainingTitle,
			TrainingType:  r.TrainingType,
			Position:      r.Position,
		}
		if r.DayOfWeek.Valid {
			v := r.DayOfWeek.Int32
			s.DayOfWeek = &v
		}
		if r.TimesPerWeek.Valid {
			v := r.TimesPerWeek.Int32
			s.TimesPerWeek = &v
		}
		result = append(result, s)
	}
	return result
}

func (h *ProgramHandler) insertSlots(ctx context.Context, qtx *db.Queries, programID pgtype.UUID, slots []ProgramSlotRequest) ([]ProgramSlotResponse, error) {
	result := make([]ProgramSlotResponse, 0, len(slots))
	for i, s := range slots {
		var trainingUUID pgtype.UUID
		if err := trainingUUID.Scan(s.TrainingID); err != nil {
			return nil, fmt.Errorf("invalid training_id at slot %d", i)
		}
		params := db.CreateCoachProgramSlotParams{
			ProgramID:  programID,
			TrainingID: trainingUUID,
			Position:   int32(i),
		}
		if s.DayOfWeek != nil {
			params.DayOfWeek = pgtype.Int4{Int32: *s.DayOfWeek, Valid: true}
		}
		if s.TimesPerWeek != nil {
			params.TimesPerWeek = pgtype.Int4{Int32: *s.TimesPerWeek, Valid: true}
		}
		row, err := qtx.CreateCoachProgramSlot(ctx, params)
		if err != nil {
			return nil, err
		}
		resp := ProgramSlotResponse{
			ID:         row.ID.String(),
			TrainingID: row.TrainingID.String(),
			Position:   row.Position,
		}
		if row.DayOfWeek.Valid {
			v := row.DayOfWeek.Int32
			resp.DayOfWeek = &v
		}
		if row.TimesPerWeek.Valid {
			v := row.TimesPerWeek.Int32
			resp.TimesPerWeek = &v
		}
		result = append(result, resp)
	}
	return result, nil
}

func (h *ProgramHandler) verifyClientEnrolled(c fiber.Ctx, coachUUID pgtype.UUID, clientIDStr string) (pgtype.UUID, bool) {
	var clientUUID pgtype.UUID
	if err := clientUUID.Scan(clientIDStr); err != nil {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid client ID"})
		return pgtype.UUID{}, false
	}
	_, err := h.queries.GetCoachEnrollment(context.Background(), db.GetCoachEnrollmentParams{
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

func programToListItem(p db.CoachProgram) ProgramListItem {
	item := ProgramListItem{
		ID:        p.ID.String(),
		CoachID:   p.CoachID.String(),
		UserID:    p.UserID.String(),
		Name:      p.Name,
		StartDate: p.StartDate.Time.Format(time.DateOnly),
		CreatedAt: p.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if p.Objective.Valid {
		item.Objective = &p.Objective.String
	}
	if p.DurationWeeks.Valid {
		v := p.DurationWeeks.Int32
		item.DurationWeeks = &v
	}
	return item
}

func buildProgramResponse(p db.CoachProgram, slots []ProgramSlotResponse) ProgramResponse {
	resp := ProgramResponse{
		ID:        p.ID.String(),
		CoachID:   p.CoachID.String(),
		UserID:    p.UserID.String(),
		Name:      p.Name,
		StartDate: p.StartDate.Time.Format(time.DateOnly),
		Slots:     slots,
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
	if resp.Slots == nil {
		resp.Slots = []ProgramSlotResponse{}
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
	for i, slot := range req.Slots {
		if err := validateSlot(slot); err != nil {
			return pgtype.Date{}, fmt.Errorf("slot %d: %s", i, err.Error())
		}
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

// CreateProgram godoc
// @Summary Create a training program for a client
// @Description Create a program with scheduled and unscheduled training slots. Requires a validated coach account and the client must be enrolled.
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

	tx, err := h.pool.Begin(context.Background())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create program"})
	}
	defer tx.Rollback(context.Background())

	qtx := h.queries.WithTx(tx)

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

	program, err := qtx.CreateCoachProgram(context.Background(), params)
	if err != nil {
		slog.Error("failed to create program", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create program"})
	}

	slots, err := h.insertSlots(context.Background(), qtx, program.ID, req.Slots)
	if err != nil {
		slog.Error("failed to insert program slots", "program_id", program.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create program slots"})
	}

	if err := tx.Commit(context.Background()); err != nil {
		slog.Error("failed to commit program transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize program"})
	}

	return c.Status(fiber.StatusCreated).JSON(buildProgramResponse(program, slots))
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

	programs, err := h.queries.GetCoachProgramsForClient(context.Background(), db.GetCoachProgramsForClientParams{
		CoachID: coachUUID,
		UserID:  clientUUID,
	})
	if err != nil {
		slog.Error("failed to retrieve programs", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve programs"})
	}

	result := make([]ProgramListItem, 0, len(programs))
	for _, p := range programs {
		result = append(result, programToListItem(p))
	}
	return c.Status(fiber.StatusOK).JSON(result)
}

// GetProgram godoc
// @Summary Get a training program
// @Description Get a program with its full slot list.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param program_id path string true "Program ID"
// @Success 200 {object} ProgramResponse "Program with slots"
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

	var programUUID pgtype.UUID
	if err := programUUID.Scan(c.Params("program_id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid program ID"})
	}

	program, err := h.queries.GetCoachProgram(context.Background(), programUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Program not found"})
		}
		slog.Error("failed to retrieve program", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve program"})
	}

	if program.CoachID.Bytes != coachUUID.Bytes || program.UserID.Bytes != clientUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	rows, err := h.queries.GetCoachProgramSlots(context.Background(), programUUID)
	if err != nil {
		slog.Error("failed to retrieve program slots", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve program slots"})
	}

	return c.Status(fiber.StatusOK).JSON(buildProgramResponse(program, buildSlotsFromRows(rows)))
}

// UpdateProgram godoc
// @Summary Update a training program
// @Description Replace program metadata and slots. Only the owning coach can update.
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

	var programUUID pgtype.UUID
	if err := programUUID.Scan(c.Params("program_id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid program ID"})
	}

	existing, err := h.queries.GetCoachProgram(context.Background(), programUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Program not found"})
		}
		slog.Error("failed to retrieve program for update", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve program"})
	}

	if existing.CoachID.Bytes != coachUUID.Bytes || existing.UserID.Bytes != clientUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	var req UpdateProgramRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	startDate, err := parseAndValidateProgramRequest(req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	tx, err := h.pool.Begin(context.Background())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update program"})
	}
	defer tx.Rollback(context.Background())

	qtx := h.queries.WithTx(tx)

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

	program, err := qtx.UpdateCoachProgram(context.Background(), updateParams)
	if err != nil {
		slog.Error("failed to update program", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update program"})
	}

	if err := qtx.DeleteCoachProgramSlots(context.Background(), programUUID); err != nil {
		slog.Error("failed to delete program slots", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update program slots"})
	}

	slots, err := h.insertSlots(context.Background(), qtx, programUUID, req.Slots)
	if err != nil {
		slog.Error("failed to insert program slots", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update program slots"})
	}

	if err := tx.Commit(context.Background()); err != nil {
		slog.Error("failed to commit program update", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize update"})
	}

	return c.Status(fiber.StatusOK).JSON(buildProgramResponse(program, slots))
}

// DeleteProgram godoc
// @Summary Delete a training program
// @Description Delete a program and its slots. Only the owning coach can delete.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param program_id path string true "Program ID"
// @Success 200 {object} map[string]string "Program deleted"
// @Failure 400 {object} map[string]string "Invalid ID"
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

	var programUUID pgtype.UUID
	if err := programUUID.Scan(c.Params("program_id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid program ID"})
	}

	program, err := h.queries.GetCoachProgram(context.Background(), programUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Program not found"})
		}
		slog.Error("failed to retrieve program for delete", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve program"})
	}

	if program.CoachID.Bytes != coachUUID.Bytes || program.UserID.Bytes != clientUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteCoachProgram(context.Background(), programUUID); err != nil {
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

	programs, err := h.queries.GetMyPrograms(context.Background(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve user programs", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve programs"})
	}

	result := make([]ProgramListItem, 0, len(programs))
	for _, p := range programs {
		result = append(result, programToListItem(p))
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
// @Success 200 {object} ProgramResponse "Program with slots"
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

	program, err := h.queries.GetCoachProgram(context.Background(), programUUID)
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

	rows, err := h.queries.GetCoachProgramSlots(context.Background(), programUUID)
	if err != nil {
		slog.Error("failed to retrieve program slots", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve program slots"})
	}

	return c.Status(fiber.StatusOK).JSON(buildProgramResponse(program, buildSlotsFromRows(rows)))
}
