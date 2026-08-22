package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
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
	ID           *string                  `json:"id"`
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
	IsLocked      bool                      `json:"is_locked"`
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

// invalidRequest marks a failure caused by the payload rather than by the
// server, so a caller unwinding a transaction can answer 400 instead of 500.
type invalidRequest struct{ error }

func invalidRequestf(format string, args ...any) error {
	return invalidRequest{fmt.Errorf(format, args...)}
}

// validateWeekSessions checks the payload and parses the session ids it carries,
// returning them positionally so an id is parsed once. Duplicates are keyed on
// the parsed uuid rather than the string, because pgtype accepts several
// spellings of the same uuid and two of them would otherwise pass as distinct
// ids and collapse onto one row.
func validateWeekSessions(sessions []WeekSessionRequest) ([]pgtype.UUID, error) {
	sessionIDs := make([]pgtype.UUID, len(sessions))
	seenIDs := make(map[pgtype.UUID]struct{}, len(sessions))
	for i, s := range sessions {
		if err := validateWeekSession(s); err != nil {
			return nil, fmt.Errorf("session %d: %w", i, err)
		}
		if s.ID == nil {
			continue
		}
		if err := sessionIDs[i].Scan(*s.ID); err != nil {
			return nil, fmt.Errorf("session %d: invalid id %s", i, *s.ID)
		}
		if _, duplicate := seenIDs[sessionIDs[i]]; duplicate {
			return nil, fmt.Errorf("session %d: id %s appears more than once", i, *s.ID)
		}
		seenIDs[sessionIDs[i]] = struct{}{}
	}
	return sessionIDs, nil
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

// checkFrozenSessions refuses a payload that would rewrite what was prescribed
// on a session the athlete already played. Only the training and its overrides
// describe the prescription, so day, notes and position stay editable, but the
// row itself must survive: dropping it would null the link the played session
// holds.
func checkFrozenSessions(ctx context.Context, qtx *db.Queries, weekID pgtype.UUID, sessions []WeekSessionRequest, sessionIDs []pgtype.UUID) error {
	// Lock the rows first: without this the athlete can play a session between
	// this read and the delete below, and the delete then nulls the link the
	// check would have refused.
	if _, err := qtx.LockCoachProgramWeekSessions(ctx, weekID); err != nil {
		return err
	}
	existing, err := qtx.GetCoachProgramWeekSessions(ctx, weekID)
	if err != nil {
		return err
	}
	frozen := make(map[pgtype.UUID]db.GetCoachProgramWeekSessionsRow)
	for _, row := range existing {
		if row.IsLocked {
			frozen[row.ID] = row
		}
	}
	if len(frozen) == 0 {
		return nil
	}

	weekOverrides, err := qtx.GetCoachProgramWeekOverrides(ctx, weekID)
	if err != nil {
		return err
	}
	prescribedOverrides := make(map[pgtype.UUID]map[pgtype.UUID][]byte, len(frozen))
	for _, o := range weekOverrides {
		if _, isFrozen := frozen[o.SessionID]; !isFrozen {
			continue
		}
		if prescribedOverrides[o.SessionID] == nil {
			prescribedOverrides[o.SessionID] = make(map[pgtype.UUID][]byte)
		}
		prescribedOverrides[o.SessionID][o.ItemID] = o.Overrides
	}

	kept := make(map[pgtype.UUID]struct{}, len(sessionIDs))
	for i, id := range sessionIDs {
		if !id.Valid {
			continue
		}
		kept[id] = struct{}{}
		row, isFrozen := frozen[id]
		if !isFrozen {
			continue
		}
		if err := checkFrozenSession(i, sessions[i], row, prescribedOverrides[id]); err != nil {
			return err
		}
	}
	for id, row := range frozen {
		if _, stillThere := kept[id]; !stillThere {
			return invalidRequestf("session %s was played by the athlete and cannot be removed from the week", row.ID.String())
		}
	}
	return nil
}

func checkFrozenSession(index int, s WeekSessionRequest, row db.GetCoachProgramWeekSessionsRow, prescribed map[pgtype.UUID][]byte) error {
	var trainingUUID pgtype.UUID
	if err := trainingUUID.Scan(s.TrainingID); err != nil {
		return invalidRequestf("invalid training_id at session %d", index)
	}
	if trainingUUID != row.TrainingID {
		return invalidRequestf("session %d: was played by the athlete, its training cannot be changed", index)
	}

	seenItems := make(map[pgtype.UUID]struct{}, len(s.Overrides))
	for _, o := range s.Overrides {
		var itemUUID pgtype.UUID
		if err := itemUUID.Scan(o.ItemID); err != nil {
			return invalidRequestf("invalid item_id in session %d override", index)
		}
		if !json.Valid(o.Overrides) {
			return invalidRequestf("invalid overrides in session %d", index)
		}
		current, prescribedForItem := prescribed[itemUUID]
		if !prescribedForItem || !sameJSON(current, o.Overrides) {
			return invalidRequestf("session %d: was played by the athlete, its overrides cannot be changed", index)
		}
		seenItems[itemUUID] = struct{}{}
	}
	if len(seenItems) != len(prescribed) {
		return invalidRequestf("session %d: was played by the athlete, its overrides cannot be changed", index)
	}
	return nil
}

// sameJSON compares two override payloads by value, because the stored jsonb
// comes back with its own key order and spacing.
func sameJSON(a, b []byte) bool {
	var aValue, bValue any
	if err := json.Unmarshal(a, &aValue); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bValue); err != nil {
		return false
	}
	return reflect.DeepEqual(aValue, bValue)
}

// syncWeekSessions reconciles a week against the payload instead of recreating
// it, so a session the client sends back by id keeps that id. Sessions played by
// the athlete reference these ids, and recreating them nulls those references.
func (h *ProgramHandler) syncWeekSessions(ctx context.Context, qtx *db.Queries, coachID, weekID pgtype.UUID, sessions []WeekSessionRequest, sessionIDs []pgtype.UUID) error {
	if err := checkFrozenSessions(ctx, qtx, weekID, sessions, sessionIDs); err != nil {
		return err
	}

	keptIDs := make([]pgtype.UUID, 0, len(sessions))
	for _, id := range sessionIDs {
		if id.Valid {
			keptIDs = append(keptIDs, id)
		}
	}

	if err := qtx.DeleteCoachProgramWeekSessionsNotIn(ctx, db.DeleteCoachProgramWeekSessionsNotInParams{
		WeekID:  weekID,
		KeptIds: keptIDs,
	}); err != nil {
		return err
	}

	for i, s := range sessions {
		session, err := h.upsertWeekSession(ctx, qtx, coachID, weekID, sessionIDs[i], i, s)
		if err != nil {
			return err
		}
		if err := h.syncSessionOverrides(ctx, qtx, session, i, s.Overrides); err != nil {
			return err
		}
	}
	return nil
}

// upsertWeekSession resolves the training against the coach, so a week can only
// ever schedule a training the coach owns. Without it the week read joins
// trainings unconditionally and hands back another coach's title and type, and
// the item scoping in syncSessionOverrides is defeated, since the foreign items
// then legitimately belong to the session's training.
func (h *ProgramHandler) upsertWeekSession(ctx context.Context, qtx *db.Queries, coachID, weekID, sessionID pgtype.UUID, index int, s WeekSessionRequest) (db.CoachProgramWeekSession, error) {
	var none db.CoachProgramWeekSession

	var trainingUUID pgtype.UUID
	if err := trainingUUID.Scan(s.TrainingID); err != nil {
		return none, invalidRequestf("invalid training_id at session %d", index)
	}
	if _, err := qtx.GetTrainingOwnedBy(ctx, db.GetTrainingOwnedByParams{
		ID:     trainingUUID,
		UserID: coachID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return none, invalidRequestf("unknown training_id at session %d", index)
	} else if err != nil {
		return none, err
	}

	var dayOfWeek, timesPerWeek pgtype.Int4
	if s.DayOfWeek != nil {
		dayOfWeek = pgtype.Int4{Int32: *s.DayOfWeek, Valid: true}
	}
	if s.TimesPerWeek != nil {
		timesPerWeek = pgtype.Int4{Int32: *s.TimesPerWeek, Valid: true}
	}
	var notes pgtype.Text
	if s.Notes != nil {
		notes = pgtype.Text{String: *s.Notes, Valid: true}
	}

	if !sessionID.Valid {
		return qtx.CreateCoachProgramWeekSession(ctx, db.CreateCoachProgramWeekSessionParams{
			WeekID:       weekID,
			TrainingID:   trainingUUID,
			DayOfWeek:    dayOfWeek,
			TimesPerWeek: timesPerWeek,
			IsEveryday:   s.IsEveryday,
			Position:     int32(index),
			Notes:        notes,
		})
	}

	session, err := qtx.UpdateCoachProgramWeekSession(ctx, db.UpdateCoachProgramWeekSessionParams{
		ID:           sessionID,
		WeekID:       weekID,
		TrainingID:   trainingUUID,
		DayOfWeek:    dayOfWeek,
		TimesPerWeek: timesPerWeek,
		IsEveryday:   s.IsEveryday,
		Position:     int32(index),
		Notes:        notes,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return none, invalidRequestf("session %d: id %s does not belong to this week", index, *s.ID)
	}
	return session, err
}

// syncSessionOverrides resolves every override against the session's own
// training, so an item id belonging to another training, and therefore possibly
// to another coach, is rejected rather than stored.
func (h *ProgramHandler) syncSessionOverrides(ctx context.Context, qtx *db.Queries, session db.CoachProgramWeekSession, index int, overrides []SessionOverrideRequest) error {
	itemIDs := make([]pgtype.UUID, len(overrides))
	for i, o := range overrides {
		if err := itemIDs[i].Scan(o.ItemID); err != nil {
			return invalidRequestf("invalid item_id in session %d override", index)
		}
	}

	if err := qtx.DeleteCoachProgramSessionOverridesNotIn(ctx, db.DeleteCoachProgramSessionOverridesNotInParams{
		SessionID:   session.ID,
		KeptItemIds: itemIDs,
	}); err != nil {
		return err
	}

	for i, o := range overrides {
		item, err := qtx.GetTrainingItemInTraining(ctx, db.GetTrainingItemInTrainingParams{
			ID:         itemIDs[i],
			TrainingID: session.TrainingID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return invalidRequestf("unknown item_id in session %d override", index)
		}
		if err != nil {
			return err
		}
		if err := validateItemOverride(itemToRequest(item), o.Overrides); err != nil {
			return invalidRequestf("session %d override on item %s: %s", index, o.ItemID, err)
		}
		if _, err := qtx.UpsertCoachProgramSessionOverride(ctx, db.UpsertCoachProgramSessionOverrideParams{
			SessionID: session.ID,
			ItemID:    itemIDs[i],
			Overrides: []byte(o.Overrides),
		}); err != nil {
			return err
		}
	}
	return nil
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
			IsLocked:      s.IsLocked,
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
// @Description Upsert a week's sessions and overrides. A session sent back with its id is updated in place and keeps that id, one sent without an id is created, and any session of the week missing from the payload is deleted. A session the athlete has already played (is_locked) is frozen: its training and overrides must be sent back unchanged and it may not be dropped from the week.
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
	sessionIDs, err := validateWeekSessions(req.Sessions)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
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

	if err := h.syncWeekSessions(c.Context(), qtx, coachUUID, week.ID, req.Sessions, sessionIDs); err != nil {
		var bad invalidRequest
		if errors.As(err, &bad) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": bad.Error()})
		}
		slog.Error("failed to sync week sessions", "week_id", week.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save week sessions"})
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
// @Description Delete a week and all its sessions/overrides (cascade). Refused when the week holds a session the athlete already played, because the cascade would null the link that session keeps to what was prescribed.
// @Tags Programs
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param program_id path string true "Program ID"
// @Param week_number path int true "Week number"
// @Success 200 {object} map[string]string "Week deleted"
// @Failure 400 {object} map[string]string "Week holds a played session"
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

	// The week cascades to its sessions and the sessions FK nulls on delete, so
	// dropping the week is the same prescription loss the week PUT refuses.
	played, err := h.queries.CountPlayedSessionsInWeek(c.Context(), db.CountPlayedSessionsInWeekParams{
		ProgramID:  programUUID,
		WeekNumber: weekNum,
	})
	if err != nil {
		slog.Error("failed to count played sessions", "program_id", programUUID.String(), "week_number", weekNum, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete week"})
	}
	if played > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "This week holds a session the athlete already played and cannot be deleted",
		})
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
