package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type SessionOverrideRequest struct {
	ItemID    string          `json:"item_id"`
	Overrides json.RawMessage `json:"overrides" swaggertype:"object"`
}

type WeekSessionRequest struct {
	TrainingID   string                   `json:"training_id"`
	DayOfWeek    *int32                   `json:"day_of_week"`
	TimesPerWeek *int32                   `json:"times_per_week"`
	IsEveryday   bool                     `json:"is_everyday"`
	Notes        *string                  `json:"notes"`
	Overrides    []SessionOverrideRequest `json:"overrides"`
}

type UpsertWeekRequest struct {
	Notes    *string              `json:"notes"`
	Sessions []WeekSessionRequest `json:"sessions"`
}

type SessionOverrideResponse struct {
	ID        string          `json:"id"`
	ItemID    string          `json:"item_id"`
	Overrides json.RawMessage `json:"overrides" swaggertype:"object"`
}

type WeekSessionResponse struct {
	ID            string                    `json:"id"`
	TrainingID    string                    `json:"training_id"`
	TrainingTitle string                    `json:"training_title"`
	TrainingType  string                    `json:"training_type"`
	DayOfWeek     *int32                    `json:"day_of_week,omitempty"`
	TimesPerWeek  *int32                    `json:"times_per_week,omitempty"`
	IsEveryday    bool                      `json:"is_everyday"`
	Position      int32                     `json:"position"`
	Notes         *string                   `json:"notes,omitempty"`
	Overrides     []SessionOverrideResponse `json:"overrides"`
}

type WeekResponse struct {
	ID         string                `json:"id"`
	ProgramID  string                `json:"program_id"`
	WeekNumber int32                 `json:"week_number"`
	Notes      *string               `json:"notes,omitempty"`
	Sessions   []WeekSessionResponse `json:"sessions"`
	CreatedAt  string                `json:"created_at"`
	UpdatedAt  string                `json:"updated_at"`
}

type WeekListItem struct {
	ID         string  `json:"id"`
	ProgramID  string  `json:"program_id"`
	WeekNumber int32   `json:"week_number"`
	Notes      *string `json:"notes,omitempty"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func validateWeekSession(s WeekSessionRequest) error {
	modes := 0
	if s.DayOfWeek != nil {
		modes++
	}
	if s.TimesPerWeek != nil {
		modes++
	}
	if s.IsEveryday {
		modes++
	}
	if modes != 1 {
		return errors.New("session must have exactly one of: day_of_week, times_per_week, or is_everyday")
	}
	if s.DayOfWeek != nil && (*s.DayOfWeek < 0 || *s.DayOfWeek > 6) {
		return errors.New("day_of_week must be between 0 (Monday) and 6 (Sunday)")
	}
	if s.TimesPerWeek != nil && *s.TimesPerWeek <= 0 {
		return errors.New("times_per_week must be greater than 0")
	}
	return nil
}

func (h *ProgramHandler) insertWeekSessions(ctx context.Context, qtx *db.Queries, weekID pgtype.UUID, sessions []WeekSessionRequest) ([]WeekSessionResponse, error) {
	result := make([]WeekSessionResponse, 0, len(sessions))
	for i, s := range sessions {
		var trainingUUID pgtype.UUID
		if err := trainingUUID.Scan(s.TrainingID); err != nil {
			return nil, fmt.Errorf("invalid training_id at session %d", i)
		}
		params := db.CreateCoachProgramWeekSessionParams{
			WeekID:     weekID,
			TrainingID: trainingUUID,
			IsEveryday: s.IsEveryday,
			Position:   int32(i),
		}
		if s.DayOfWeek != nil {
			params.DayOfWeek = pgtype.Int4{Int32: *s.DayOfWeek, Valid: true}
		}
		if s.TimesPerWeek != nil {
			params.TimesPerWeek = pgtype.Int4{Int32: *s.TimesPerWeek, Valid: true}
		}
		if s.Notes != nil {
			params.Notes = pgtype.Text{String: *s.Notes, Valid: true}
		}
		session, err := qtx.CreateCoachProgramWeekSession(ctx, params)
		if err != nil {
			return nil, err
		}

		overrides := make([]SessionOverrideResponse, 0, len(s.Overrides))
		for _, o := range s.Overrides {
			var itemUUID pgtype.UUID
			if err := itemUUID.Scan(o.ItemID); err != nil {
				return nil, fmt.Errorf("invalid item_id in session %d override", i)
			}
			override, err := qtx.UpsertCoachProgramSessionOverride(ctx, db.UpsertCoachProgramSessionOverrideParams{
				SessionID: session.ID,
				ItemID:    itemUUID,
				Overrides: []byte(o.Overrides),
			})
			if err != nil {
				return nil, err
			}
			overrides = append(overrides, SessionOverrideResponse{
				ID:        override.ID.String(),
				ItemID:    override.ItemID.String(),
				Overrides: json.RawMessage(override.Overrides),
			})
		}

		resp := WeekSessionResponse{
			ID:            session.ID.String(),
			TrainingID:    session.TrainingID.String(),
			TrainingTitle: "",
			TrainingType:  "",
			IsEveryday:    session.IsEveryday,
			Position:      session.Position,
			Overrides:     overrides,
		}
		if session.DayOfWeek.Valid {
			v := session.DayOfWeek.Int32
			resp.DayOfWeek = &v
		}
		if session.TimesPerWeek.Valid {
			v := session.TimesPerWeek.Int32
			resp.TimesPerWeek = &v
		}
		if session.Notes.Valid {
			resp.Notes = &session.Notes.String
		}
		result = append(result, resp)
	}
	return result, nil
}

func buildWeekResponse(week db.CoachProgramWeek, sessions []db.GetCoachProgramWeekSessionsRow, overrides []db.CoachProgramSessionOverride) WeekResponse {
	overridesBySession := make(map[pgtype.UUID][]SessionOverrideResponse, len(overrides))
	for _, o := range overrides {
		overridesBySession[o.SessionID] = append(overridesBySession[o.SessionID], SessionOverrideResponse{
			ID:        o.ID.String(),
			ItemID:    o.ItemID.String(),
			Overrides: json.RawMessage(o.Overrides),
		})
	}

	sessionResps := make([]WeekSessionResponse, 0, len(sessions))
	for _, s := range sessions {
		resp := WeekSessionResponse{
			ID:            s.ID.String(),
			TrainingID:    s.TrainingID.String(),
			TrainingTitle: s.TrainingTitle,
			TrainingType:  s.TrainingType,
			IsEveryday:    s.IsEveryday,
			Position:      s.Position,
			Overrides:     overridesBySession[s.ID],
		}
		if resp.Overrides == nil {
			resp.Overrides = []SessionOverrideResponse{}
		}
		if s.DayOfWeek.Valid {
			v := s.DayOfWeek.Int32
			resp.DayOfWeek = &v
		}
		if s.TimesPerWeek.Valid {
			v := s.TimesPerWeek.Int32
			resp.TimesPerWeek = &v
		}
		if s.SessionNotes.Valid {
			resp.Notes = &s.SessionNotes.String
		}
		sessionResps = append(sessionResps, resp)
	}

	resp := WeekResponse{
		ID:         week.ID.String(),
		ProgramID:  week.ProgramID.String(),
		WeekNumber: week.WeekNumber,
		Sessions:   sessionResps,
		CreatedAt:  week.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:  week.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if resp.Sessions == nil {
		resp.Sessions = []WeekSessionResponse{}
	}
	if week.Notes.Valid {
		resp.Notes = &week.Notes.String
	}
	return resp
}

func weekToListItem(w db.CoachProgramWeek) WeekListItem {
	item := WeekListItem{
		ID:         w.ID.String(),
		ProgramID:  w.ProgramID.String(),
		WeekNumber: w.WeekNumber,
		CreatedAt:  w.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:  w.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if w.Notes.Valid {
		item.Notes = &w.Notes.String
	}
	return item
}

func parseWeekNumber(s string) (int32, error) {
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n <= 0 {
		return 0, errors.New("week_number must be a positive integer")
	}
	return int32(n), nil
}

func (h *ProgramHandler) loadWeekData(ctx context.Context, weekID pgtype.UUID) ([]db.GetCoachProgramWeekSessionsRow, []db.CoachProgramSessionOverride, error) {
	sessions, err := h.queries.GetCoachProgramWeekSessions(ctx, weekID)
	if err != nil {
		return nil, nil, err
	}
	overrides, err := h.queries.GetCoachProgramWeekOverrides(ctx, weekID)
	if err != nil {
		return nil, nil, err
	}
	return sessions, overrides, nil
}

// UpsertWeek godoc
// @Summary Create or replace a program week
// @Description Upsert a week's sessions and overrides. Existing sessions for the week are replaced.
// @Tags Programs
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param program_id path string true "Program ID"
// @Param week_number path int true "Week number (>= 1)"
// @Param request body UpsertWeekRequest true "Week data"
// @Success 200 {object} WeekResponse "Upserted week"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Program not found"
// @Router /api/coach/clients/{user_id}/programs/{program_id}/weeks/{week_number} [put]
func (h *ProgramHandler) UpsertWeek(c fiber.Ctx) error {
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

	weekNum, err := parseWeekNumber(c.Params("week_number"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	var req UpsertWeekRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	for i, s := range req.Sessions {
		if err := validateWeekSession(s); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("session %d: %s", i, err.Error())})
		}
	}

	tx, err := h.pool.Begin(c.Context())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to upsert week"})
	}
	defer tx.Rollback(c.Context())

	qtx := h.queries.WithTx(tx)

	upsertParams := db.UpsertCoachProgramWeekParams{
		ProgramID:  programUUID,
		WeekNumber: weekNum,
	}
	if req.Notes != nil {
		upsertParams.Notes = pgtype.Text{String: *req.Notes, Valid: true}
	}
	week, err := qtx.UpsertCoachProgramWeek(c.Context(), upsertParams)
	if err != nil {
		slog.Error("failed to upsert week", "program_id", programUUID.String(), "week_number", weekNum, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to upsert week"})
	}

	if err := qtx.DeleteCoachProgramWeekSessions(c.Context(), week.ID); err != nil {
		slog.Error("failed to delete week sessions", "week_id", week.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to replace week sessions"})
	}

	if _, err := h.insertWeekSessions(c.Context(), qtx, week.ID, req.Sessions); err != nil {
		slog.Error("failed to insert week sessions", "week_id", week.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create week sessions"})
	}

	if err := tx.Commit(c.Context()); err != nil {
		slog.Error("failed to commit week upsert", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize week"})
	}

	sessions, overrides, err := h.loadWeekData(c.Context(), week.ID)
	if err != nil {
		slog.Error("failed to load week data after upsert", "week_id", week.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve week"})
	}

	return c.Status(fiber.StatusOK).JSON(buildWeekResponse(week, sessions, overrides))
}

// GetWeeks godoc
// @Summary List weeks of a program
// @Description Get all explicitly defined weeks for a program (summary, no sessions).
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param program_id path string true "Program ID"
// @Success 200 {array} WeekListItem "List of weeks"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Program not found"
// @Router /api/coach/clients/{user_id}/programs/{program_id}/weeks [get]
func (h *ProgramHandler) GetWeeks(c fiber.Ctx) error {
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

	weeks, err := h.queries.GetCoachProgramWeeks(c.Context(), programUUID)
	if err != nil {
		slog.Error("failed to retrieve program weeks", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve weeks"})
	}

	result := make([]WeekListItem, 0, len(weeks))
	for _, w := range weeks {
		result = append(result, weekToListItem(w))
	}
	return c.Status(fiber.StatusOK).JSON(result)
}

// GetWeek godoc
// @Summary Get a program week
// @Description Get a specific week with its sessions and per-item overrides.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param program_id path string true "Program ID"
// @Param week_number path int true "Week number"
// @Success 200 {object} WeekResponse "Week with sessions and overrides"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Program or week not found"
// @Router /api/coach/clients/{user_id}/programs/{program_id}/weeks/{week_number} [get]
func (h *ProgramHandler) GetWeek(c fiber.Ctx) error {
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

	weekNum, err := parseWeekNumber(c.Params("week_number"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	week, err := h.queries.GetCoachProgramWeek(c.Context(), db.GetCoachProgramWeekParams{
		ProgramID:  programUUID,
		WeekNumber: weekNum,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Week not found"})
		}
		slog.Error("failed to retrieve week", "program_id", programUUID.String(), "week_number", weekNum, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve week"})
	}

	sessions, overrides, err := h.loadWeekData(c.Context(), week.ID)
	if err != nil {
		slog.Error("failed to load week data", "week_id", week.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve week"})
	}

	return c.Status(fiber.StatusOK).JSON(buildWeekResponse(week, sessions, overrides))
}

// DeleteWeek godoc
// @Summary Delete a program week
// @Description Delete a week and all its sessions/overrides (cascade).
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param program_id path string true "Program ID"
// @Param week_number path int true "Week number"
// @Success 200 {object} map[string]string "Week deleted"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Program or week not found"
// @Router /api/coach/clients/{user_id}/programs/{program_id}/weeks/{week_number} [delete]
func (h *ProgramHandler) DeleteWeek(c fiber.Ctx) error {
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

	weekNum, err := parseWeekNumber(c.Params("week_number"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if err := h.queries.DeleteCoachProgramWeek(c.Context(), db.DeleteCoachProgramWeekParams{
		ProgramID:  programUUID,
		WeekNumber: weekNum,
	}); err != nil {
		slog.Error("failed to delete week", "program_id", programUUID.String(), "week_number", weekNum, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete week"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Week deleted successfully"})
}

// GetMyWeeks godoc
// @Summary List weeks of one of my programs
// @Description Get all defined weeks for a program assigned to the authenticated user.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param program_id path string true "Program ID"
// @Success 200 {array} WeekListItem "List of weeks"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Program not found"
// @Router /api/user/programs/{program_id}/weeks [get]
func (h *ProgramHandler) GetMyWeeks(c fiber.Ctx) error {
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

	weeks, err := h.queries.GetCoachProgramWeeks(c.Context(), programUUID)
	if err != nil {
		slog.Error("failed to retrieve program weeks", "program_id", programUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve weeks"})
	}

	result := make([]WeekListItem, 0, len(weeks))
	for _, w := range weeks {
		result = append(result, weekToListItem(w))
	}
	return c.Status(fiber.StatusOK).JSON(result)
}

// GetMyWeek godoc
// @Summary Get a week from one of my programs
// @Description Get a specific week with sessions and overrides for a program assigned to the authenticated user.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param program_id path string true "Program ID"
// @Param week_number path int true "Week number"
// @Success 200 {object} WeekResponse "Week with sessions and overrides"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Program or week not found"
// @Router /api/user/programs/{program_id}/weeks/{week_number} [get]
func (h *ProgramHandler) GetMyWeek(c fiber.Ctx) error {
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

	weekNum, err := parseWeekNumber(c.Params("week_number"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	week, err := h.queries.GetCoachProgramWeek(c.Context(), db.GetCoachProgramWeekParams{
		ProgramID:  programUUID,
		WeekNumber: weekNum,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Week not found"})
		}
		slog.Error("failed to retrieve week", "program_id", programUUID.String(), "week_number", weekNum, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve week"})
	}

	sessions, overrides, err := h.loadWeekData(c.Context(), week.ID)
	if err != nil {
		slog.Error("failed to load week data", "week_id", week.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve week"})
	}

	return c.Status(fiber.StatusOK).JSON(buildWeekResponse(week, sessions, overrides))
}
