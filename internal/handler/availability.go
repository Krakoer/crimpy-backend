package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	daysInWeek                = 7
	maxAvailabilityNoteLength = 2000
)

type AvailabilityHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewAvailabilityHandler(queries *db.Queries, pool *pgxpool.Pool) *AvailabilityHandler {
	return &AvailabilityHandler{queries: queries, pool: pool}
}

type DayAvailabilityRequest struct {
	DayOfWeek       int32   `json:"day_of_week"`
	IsAvailable     bool    `json:"is_available"`
	DurationMinutes *int32  `json:"duration_minutes"`
	Note            *string `json:"note"`
}

type WeekAvailabilityRequest struct {
	Days []DayAvailabilityRequest `json:"days"`
}

type DayAvailabilityResponse struct {
	DayOfWeek       int32   `json:"day_of_week"`
	IsAvailable     bool    `json:"is_available"`
	DurationMinutes *int32  `json:"duration_minutes,omitempty"`
	Note            *string `json:"note,omitempty"`
}

type WeekAvailabilityResponse struct {
	UserID    string                    `json:"user_id"`
	WeekStart string                    `json:"week_start"`
	UpdatedAt string                    `json:"updated_at"`
	Days      []DayAvailabilityResponse `json:"days"`
}

type AvailabilityReminderRequest struct {
	Enabled   bool  `json:"enabled"`
	DayOfWeek int32 `json:"day_of_week"`
	Hour      int32 `json:"hour"`
	Minute    int32 `json:"minute"`
}

type AvailabilityReminderResponse struct {
	Enabled   bool  `json:"enabled"`
	DayOfWeek int32 `json:"day_of_week"`
	Hour      int32 `json:"hour"`
	Minute    int32 `json:"minute"`
}

func dayAvailabilityToResponse(d db.CoacheeDayAvailability) DayAvailabilityResponse {
	resp := DayAvailabilityResponse{
		DayOfWeek:   d.DayOfWeek,
		IsAvailable: d.IsAvailable,
	}
	if d.DurationMinutes.Valid {
		v := d.DurationMinutes.Int32
		resp.DurationMinutes = &v
	}
	if d.Note.Valid {
		resp.Note = &d.Note.String
	}
	return resp
}

// availabilityRowsToWeeks groups day rows into one response per week. The rows
// must already be ordered by week_start then day_of_week, which every query
// returning them does.
func availabilityRowsToWeeks(rows []db.CoacheeDayAvailability) []WeekAvailabilityResponse {
	weeks := []WeekAvailabilityResponse{}
	for _, row := range rows {
		weekStart := row.WeekStart.Time.Format(time.DateOnly)
		updatedAt := row.UpdatedAt.Time.UTC().Format(time.RFC3339)
		if len(weeks) == 0 || weeks[len(weeks)-1].WeekStart != weekStart {
			weeks = append(weeks, WeekAvailabilityResponse{
				UserID:    row.UserID.String(),
				WeekStart: weekStart,
				UpdatedAt: updatedAt,
				Days:      []DayAvailabilityResponse{},
			})
		}
		week := &weeks[len(weeks)-1]
		// The week reads as edited when its most recently touched day was. The
		// string compare holds because every value goes through the same
		// fixed width RFC3339 UTC format, where lexical order is chronological.
		if updatedAt > week.UpdatedAt {
			week.UpdatedAt = updatedAt
		}
		week.Days = append(week.Days, dayAvailabilityToResponse(row))
	}
	return weeks
}

func reminderToResponse(r db.CoachAvailabilityReminder) AvailabilityReminderResponse {
	return AvailabilityReminderResponse{
		Enabled:   r.Enabled,
		DayOfWeek: r.DayOfWeek,
		Hour:      r.Hour,
		Minute:    r.Minute,
	}
}

// parseWeekStart accepts the Monday of a calendar week. Program weeks run Monday
// to Sunday, so a declaration keyed to any other day would never line up with a
// week the coach edits.
func parseWeekStart(raw string) (pgtype.Date, error) {
	t, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return pgtype.Date{}, errors.New("week_start must be in YYYY-MM-DD format")
	}
	if t.Weekday() != time.Monday {
		return pgtype.Date{}, errors.New("week_start must be a Monday")
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

// validateWeekAvailability demands the whole week rather than the days that
// changed: the presence of a week is what tells the reminder the coachee has
// declared it, so a partial write would leave that ambiguous.
func validateWeekAvailability(days []DayAvailabilityRequest) error {
	if len(days) != daysInWeek {
		return fmt.Errorf("days must carry all %d days of the week", daysInWeek)
	}
	seen := make(map[int32]bool, daysInWeek)
	for _, day := range days {
		if day.DayOfWeek < 0 || day.DayOfWeek > 6 {
			return errors.New("day_of_week must be between 0 (Monday) and 6 (Sunday)")
		}
		if seen[day.DayOfWeek] {
			return fmt.Errorf("day_of_week %d is declared twice", day.DayOfWeek)
		}
		seen[day.DayOfWeek] = true
		if day.DurationMinutes != nil && *day.DurationMinutes <= 0 {
			return errors.New("duration_minutes must be greater than 0")
		}
		if day.Note != nil && utf8.RuneCountInString(*day.Note) > maxAvailabilityNoteLength {
			return fmt.Errorf("note must be at most %d characters", maxAvailabilityNoteLength)
		}
	}
	return nil
}

func validateReminderRequest(req AvailabilityReminderRequest) error {
	if req.DayOfWeek < 0 || req.DayOfWeek > 6 {
		return errors.New("day_of_week must be between 0 (Monday) and 6 (Sunday)")
	}
	if req.Hour < 0 || req.Hour > 23 {
		return errors.New("hour must be between 0 and 23")
	}
	if req.Minute < 0 || req.Minute > 59 {
		return errors.New("minute must be between 0 and 59")
	}
	return nil
}

// requireCallerUUID reads the authenticated user from the request context. A
// subject that is not a UUID means a malformed token rather than a bad body,
// so it answers 401 the way the shared ownership helper does.
func requireCallerUUID(c fiber.Ctx) (pgtype.UUID, bool) {
	var userUUID pgtype.UUID
	if err := userUUID.Scan(middleware.GetUserID(c)); err != nil {
		c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid user ID"})
		return pgtype.UUID{}, false
	}
	return userUUID, true
}

// GetMyAvailability godoc
// @Summary Get my declared availability
// @Description Retrieve every calendar week the authenticated user has declared their availability for.
// @Tags Availability
// @Produce json
// @Security BearerAuth
// @Success 200 {array} WeekAvailabilityResponse "Declared weeks"
// @Failure 401 {object} map[string]string "Invalid user ID"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/user/availability [get]
func (h *AvailabilityHandler) GetMyAvailability(c fiber.Ctx) error {
	userUUID, ok := requireCallerUUID(c)
	if !ok {
		return nil
	}

	rows, err := h.queries.GetCoacheeAvailability(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve availability", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve availability"})
	}

	return c.Status(fiber.StatusOK).JSON(availabilityRowsToWeeks(rows))
}

// UpsertMyWeekAvailability godoc
// @Summary Declare my availability for a calendar week
// @Description Replace the authenticated user's availability for one calendar week. The body must carry all seven days, day_of_week 0 = Monday to 6 = Sunday. A day declared unavailable keeps its note and drops its duration.
// @Tags Availability
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param week_start path string true "Monday of the week, YYYY-MM-DD"
// @Param request body WeekAvailabilityRequest true "The seven days of the week"
// @Success 200 {object} WeekAvailabilityResponse "The declared week"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/user/availability/{week_start} [put]
func (h *AvailabilityHandler) UpsertMyWeekAvailability(c fiber.Ctx) error {
	userUUID, ok := requireCallerUUID(c)
	if !ok {
		return nil
	}

	weekStart, err := parseWeekStart(c.Params("week_start"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	var req WeekAvailabilityRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if err := validateWeekAvailability(req.Days); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Written in a fixed order rather than the order the body listed them, so
	// two concurrent writes of the same week take the row locks the same way
	// round and cannot deadlock each other.
	sort.Slice(req.Days, func(i, j int) bool {
		return req.Days[i].DayOfWeek < req.Days[j].DayOfWeek
	})

	tx, err := h.pool.Begin(c.Context())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save availability"})
	}
	defer tx.Rollback(c.Context())

	qtx := h.queries.WithTx(tx)
	for _, day := range req.Days {
		params := db.UpsertCoacheeDayAvailabilityParams{
			UserID:      userUUID,
			WeekStart:   weekStart,
			DayOfWeek:   day.DayOfWeek,
			IsAvailable: day.IsAvailable,
		}
		// A duration on a day the athlete cannot train has no reading, and the
		// app leaves one behind when a filled day is toggled off. The note is
		// kept: "travelling" is worth saying about a day that is a no.
		if day.IsAvailable && day.DurationMinutes != nil {
			params.DurationMinutes = pgtype.Int4{Int32: *day.DurationMinutes, Valid: true}
		}
		if day.Note != nil {
			params.Note = pgtype.Text{String: *day.Note, Valid: true}
		}
		if _, err := qtx.UpsertCoacheeDayAvailability(c.Context(), params); err != nil {
			slog.Error("failed to upsert availability day", "user_id", userUUID.String(), "day_of_week", day.DayOfWeek, "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save availability"})
		}
	}

	if err := tx.Commit(c.Context()); err != nil {
		slog.Error("failed to commit availability", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize availability"})
	}

	return h.respondWithWeek(c, userUUID, weekStart)
}

func (h *AvailabilityHandler) respondWithWeek(c fiber.Ctx, userUUID pgtype.UUID, weekStart pgtype.Date) error {
	rows, err := h.queries.GetCoacheeWeekAvailability(c.Context(), db.GetCoacheeWeekAvailabilityParams{
		UserID:    userUUID,
		WeekStart: weekStart,
	})
	if err != nil {
		slog.Error("failed to reload availability", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve availability"})
	}
	weeks := availabilityRowsToWeeks(rows)
	if len(weeks) == 0 {
		// The seven rows were committed a moment ago, so an empty read is a
		// broken invariant rather than a week the client should handle.
		slog.Error("availability read back empty after a committed write", "user_id", userUUID.String())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve availability"})
	}
	return c.Status(fiber.StatusOK).JSON(weeks[0])
}

// GetMyAvailabilityReminder godoc
// @Summary Get the availability reminder my coach configured
// @Description Retrieve the weekly reminder the authenticated user's coach set. The hour is a wall clock time the athlete app raises in the athlete's own timezone.
// @Tags Availability
// @Produce json
// @Security BearerAuth
// @Success 200 {object} AvailabilityReminderResponse "The reminder"
// @Failure 401 {object} map[string]string "Invalid user ID"
// @Failure 404 {object} map[string]string "No coach, or no reminder configured"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/user/availability-reminder [get]
func (h *AvailabilityHandler) GetMyAvailabilityReminder(c fiber.Ctx) error {
	userUUID, ok := requireCallerUUID(c)
	if !ok {
		return nil
	}

	reminder, err := h.queries.GetAvailabilityReminderForUser(c.Context(), userUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "No availability reminder"})
		}
		slog.Error("failed to retrieve availability reminder", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve reminder"})
	}

	return c.Status(fiber.StatusOK).JSON(reminderToResponse(reminder))
}

// GetClientAvailability godoc
// @Summary Get a client's declared availability
// @Description Retrieve every calendar week a user enrolled with the authenticated coach has declared.
// @Tags Availability
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Success 200 {array} WeekAvailabilityResponse "Declared weeks"
// @Failure 400 {object} map[string]string "Invalid client ID"
// @Failure 403 {object} map[string]string "Not a validated coach or client not enrolled"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/coach/clients/{user_id}/availability [get]
func (h *AvailabilityHandler) GetClientAvailability(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}
	clientUUID, ok := verifyClientEnrolled(c, h.queries, coachUUID, c.Params("user_id"))
	if !ok {
		return nil
	}

	rows, err := h.queries.GetCoacheeAvailability(c.Context(), clientUUID)
	if err != nil {
		slog.Error("failed to retrieve client availability", "client_id", clientUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve availability"})
	}

	return c.Status(fiber.StatusOK).JSON(availabilityRowsToWeeks(rows))
}

// GetAvailabilityReminder godoc
// @Summary Get my availability reminder
// @Description Retrieve the weekly reminder the authenticated coach configured for their coachees.
// @Tags Availability
// @Produce json
// @Security BearerAuth
// @Success 200 {object} AvailabilityReminderResponse "The reminder"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 404 {object} map[string]string "No reminder configured"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/coach/availability-reminder [get]
func (h *AvailabilityHandler) GetAvailabilityReminder(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	reminder, err := h.queries.GetCoachAvailabilityReminder(c.Context(), coachUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "No availability reminder"})
		}
		slog.Error("failed to retrieve availability reminder", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve reminder"})
	}

	return c.Status(fiber.StatusOK).JSON(reminderToResponse(reminder))
}

// SetAvailabilityReminder godoc
// @Summary Set my availability reminder
// @Description Configure the weekly reminder nudging coachees who have not declared the coming week. day_of_week is 0 = Monday to 6 = Sunday. The hour is a wall clock time each athlete's app raises in that athlete's own timezone, not the coach's.
// @Tags Availability
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body AvailabilityReminderRequest true "Reminder settings"
// @Success 200 {object} AvailabilityReminderResponse "The saved reminder"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/coach/availability-reminder [put]
func (h *AvailabilityHandler) SetAvailabilityReminder(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var req AvailabilityReminderRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if err := validateReminderRequest(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	reminder, err := h.queries.UpsertCoachAvailabilityReminder(c.Context(), db.UpsertCoachAvailabilityReminderParams{
		CoachID:   coachUUID,
		Enabled:   req.Enabled,
		DayOfWeek: req.DayOfWeek,
		Hour:      req.Hour,
		Minute:    req.Minute,
	})
	if err != nil {
		slog.Error("failed to save availability reminder", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save reminder"})
	}

	return c.Status(fiber.StatusOK).JSON(reminderToResponse(reminder))
}
