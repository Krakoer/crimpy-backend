package handler

import (
	"crimpy/backend/internal/db"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultFeedLimit     = 20
	maxFeedLimit         = 100
	maxPendingFeedback   = 50
	maxTimezoneOffsetMin = 14 * 60

	defaultEmptyWeekDayOfWeek = 4
	defaultEmptyWeekHour      = 21
)

type CoachTodoHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewCoachTodoHandler(queries *db.Queries, pool *pgxpool.Pool) *CoachTodoHandler {
	return &CoachTodoHandler{queries: queries, pool: pool}
}

type FeedEventResponse struct {
	Kind          string  `json:"kind"`
	OccurredAt    string  `json:"occurred_at"`
	UserID        string  `json:"user_id"`
	UserFirstname string  `json:"user_firstname"`
	UserLastname  string  `json:"user_lastname"`
	SessionID     *string `json:"session_id,omitempty"`
	Title         *string `json:"title,omitempty"`
	Activity      *int32  `json:"activity,omitempty"`
	Origin        *string `json:"origin,omitempty"`
	Note          *string `json:"note,omitempty"`
	WeekStart     *string `json:"week_start,omitempty"`
}

type PendingFeedbackResponse struct {
	SessionID     string `json:"session_id"`
	UserID        string `json:"user_id"`
	UserFirstname string `json:"user_firstname"`
	UserLastname  string `json:"user_lastname"`
	SessionName   string `json:"session_name"`
	SessionDate   string `json:"session_date"`
	Activity      int32  `json:"activity"`
	Notes         string `json:"notes"`
}

type EmptyProgramWeekResponse struct {
	ProgramID     string `json:"program_id"`
	ProgramName   string `json:"program_name"`
	UserID        string `json:"user_id"`
	UserFirstname string `json:"user_firstname"`
	UserLastname  string `json:"user_lastname"`
	WeekNumber    int32  `json:"week_number"`
	WeekStart     string `json:"week_start"`
}

// EmptyWeekCheckResponse tells the portal what the empty week list is about,
// so a coach reading an empty list knows whether nothing is missing or the
// moment they chose has simply not come round yet.
type EmptyWeekCheckResponse struct {
	DayOfWeek int32  `json:"day_of_week"`
	Hour      int32  `json:"hour"`
	Minute    int32  `json:"minute"`
	Reached   bool   `json:"reached"`
	WeekStart string `json:"week_start"`
}

type CoachTodoResponse struct {
	PendingFeedback []PendingFeedbackResponse  `json:"pending_feedback"`
	EmptyWeeks      []EmptyProgramWeekResponse `json:"empty_weeks"`
	EmptyWeekCheck  EmptyWeekCheckResponse     `json:"empty_week_check"`
	// How many sessions the coach's athletes did in the current week. Carried
	// here rather than on its own route because the week it counts is the local
	// one this request already had to place to judge the empty week check.
	SessionsThisWeek int64 `json:"sessions_this_week"`
}

type CoachTodoSettingsRequest struct {
	EmptyWeekDayOfWeek int32 `json:"empty_week_day_of_week"`
	EmptyWeekHour      int32 `json:"empty_week_hour"`
	EmptyWeekMinute    int32 `json:"empty_week_minute"`
}

type CoachTodoSettingsResponse struct {
	EmptyWeekDayOfWeek int32 `json:"empty_week_day_of_week"`
	EmptyWeekHour      int32 `json:"empty_week_hour"`
	EmptyWeekMinute    int32 `json:"empty_week_minute"`
}

func todoSettingsToResponse(s db.CoachTodoSetting) CoachTodoSettingsResponse {
	return CoachTodoSettingsResponse{
		EmptyWeekDayOfWeek: s.EmptyWeekDayOfWeek,
		EmptyWeekHour:      s.EmptyWeekHour,
		EmptyWeekMinute:    s.EmptyWeekMinute,
	}
}

// defaultTodoSettings is what a coach who never opened the settings is read at.
// Friday evening is the moment the ticket names, and a default the whole feature
// works from beats answering that nothing was configured.
func defaultTodoSettings() CoachTodoSettingsResponse {
	return CoachTodoSettingsResponse{
		EmptyWeekDayOfWeek: defaultEmptyWeekDayOfWeek,
		EmptyWeekHour:      defaultEmptyWeekHour,
		EmptyWeekMinute:    0,
	}
}

func validateTodoSettings(req CoachTodoSettingsRequest) error {
	if req.EmptyWeekDayOfWeek < 0 || req.EmptyWeekDayOfWeek > 6 {
		return errors.New("empty_week_day_of_week must be between 0 (Monday) and 6 (Sunday)")
	}
	if req.EmptyWeekHour < 0 || req.EmptyWeekHour > 23 {
		return errors.New("empty_week_hour must be between 0 and 23")
	}
	if req.EmptyWeekMinute < 0 || req.EmptyWeekMinute > 59 {
		return errors.New("empty_week_minute must be between 0 and 59")
	}
	return nil
}

// parseTimezoneOffset reads the caller's offset east of UTC in minutes, which
// is what a browser sends as the negation of getTimezoneOffset(). The moment a
// coach picked is a wall clock time in their own week, so the server cannot
// judge it from its own clock alone.
func parseTimezoneOffset(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	minutes, err := strconv.Atoi(raw)
	if err != nil || minutes < -maxTimezoneOffsetMin || minutes > maxTimezoneOffsetMin {
		return 0, errors.New("tz_offset_minutes must be a whole number of minutes between -840 and 840")
	}
	return time.Duration(minutes) * time.Minute, nil
}

// mondayOfWeek truncates an instant to the Monday of the week holding it, at
// midnight. day_of_week is 0 = Monday everywhere in this codebase, while Go
// counts from Sunday, which is what the shift corrects.
func mondayOfWeek(t time.Time) time.Time {
	daysSinceMonday := (int(t.Weekday()) + 6) % 7
	day := t.AddDate(0, 0, -daysSinceMonday)
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, t.Location())
}

// GetCoachFeed godoc
// @Summary Get my coaching feed
// @Description Recent activity across every coachee enrolled with the authenticated coach, newest first. Each event carries a kind: session_completed for a session an athlete did, availability_declared for a calendar week they declared, coachee_enrolled for a coachee who joined. Pass before to page backwards, using the occurred_at of the oldest event already held.
// @Tags Coaching
// @Produce json
// @Security BearerAuth
// @Param limit query int false "Events to return, 1 to 100, defaults to 20"
// @Param before query string false "Only events strictly older than this RFC3339 instant"
// @Success 200 {array} FeedEventResponse "Feed events"
// @Failure 400 {object} map[string]string "Invalid before cursor"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/coach/feed [get]
func (h *CoachTodoHandler) GetCoachFeed(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	limit := int32(defaultFeedLimit)
	if v, err := strconv.ParseInt(c.Query("limit", strconv.Itoa(defaultFeedLimit)), 10, 32); err == nil && v > 0 && v <= maxFeedLimit {
		limit = int32(v)
	}

	var before pgtype.Timestamptz
	if raw := c.Query("before", ""); raw != "" {
		instant, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "before must be an RFC3339 instant"})
		}
		before = pgtype.Timestamptz{Time: instant, Valid: true}
	}

	rows, err := h.queries.GetCoachFeed(c.Context(), db.GetCoachFeedParams{
		CoachID:  coachUUID,
		RowLimit: limit,
		Before:   before,
	})
	if err != nil {
		slog.Error("failed to retrieve coach feed", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve feed"})
	}

	events := make([]FeedEventResponse, 0, len(rows))
	for _, row := range rows {
		events = append(events, feedRowToResponse(row))
	}

	return c.Status(fiber.StatusOK).JSON(events)
}

func feedRowToResponse(row db.GetCoachFeedRow) FeedEventResponse {
	event := FeedEventResponse{
		Kind:          row.Kind,
		OccurredAt:    row.OccurredAt.Time.UTC().Format(time.RFC3339),
		UserID:        row.UserID.String(),
		UserFirstname: row.UserFirstname,
		UserLastname:  row.UserLastname,
	}
	if row.SessionID.Valid {
		id := row.SessionID.String()
		event.SessionID = &id
	}
	if row.Title.Valid {
		event.Title = &row.Title.String
	}
	if row.Activity.Valid {
		activity := row.Activity.Int32
		event.Activity = &activity
	}
	if row.Origin.Valid {
		event.Origin = &row.Origin.String
	}
	if row.Note.Valid {
		event.Note = &row.Note.String
	}
	if row.WeekStart.Valid {
		weekStart := row.WeekStart.Time.Format(time.DateOnly)
		event.WeekStart = &weekStart
	}
	return event
}

// GetCoachTodo godoc
// @Summary Get my coaching TODO list
// @Description What the authenticated coach still owes their coachees: the sessions whose notes have no answer yet, and the programs whose next calendar week holds no session, plus how many sessions their athletes did this week. The empty weeks only appear once the weekly moment the coach configured has passed in their own week, which is why the caller sends its UTC offset.
// @Tags Coaching
// @Produce json
// @Security BearerAuth
// @Param tz_offset_minutes query int false "Caller's offset east of UTC in minutes, defaults to 0"
// @Success 200 {object} CoachTodoResponse "The TODO list"
// @Failure 400 {object} map[string]string "Invalid timezone offset"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/coach/todo [get]
func (h *CoachTodoHandler) GetCoachTodo(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	offset, err := parseTimezoneOffset(c.Query("tz_offset_minutes", ""))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	settings, err := h.todoSettings(c, coachUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve TODO settings"})
	}

	pending, err := h.queries.GetCoachPendingSessionFeedback(c.Context(), db.GetCoachPendingSessionFeedbackParams{
		CoachID:  coachUUID,
		RowLimit: maxPendingFeedback,
	})
	if err != nil {
		slog.Error("failed to retrieve pending session feedback", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve TODO list"})
	}

	// The clock is shifted rather than located, so every field below reads as
	// the coach's own wall clock without the server needing their timezone name.
	coachNow := time.Now().UTC().Add(offset)
	thisMonday := mondayOfWeek(coachNow)
	checkMoment := thisMonday.
		AddDate(0, 0, int(settings.EmptyWeekDayOfWeek)).
		Add(time.Duration(settings.EmptyWeekHour)*time.Hour + time.Duration(settings.EmptyWeekMinute)*time.Minute)
	nextMonday := thisMonday.AddDate(0, 0, daysInWeek)

	// Back out of the shifted clock to name the two real instants the week runs
	// between, since the rows being counted are stored in UTC.
	weekStart := thisMonday.Add(-offset)
	sessionsThisWeek, err := h.queries.CountCoachSessionsInWindow(c.Context(), db.CountCoachSessionsInWindowParams{
		CoachID:     coachUUID,
		WindowStart: pgtype.Timestamptz{Time: weekStart, Valid: true},
		WindowEnd:   pgtype.Timestamptz{Time: weekStart.AddDate(0, 0, daysInWeek), Valid: true},
	})
	if err != nil {
		slog.Error("failed to count sessions this week", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve TODO list"})
	}

	response := CoachTodoResponse{
		PendingFeedback:  make([]PendingFeedbackResponse, 0, len(pending)),
		EmptyWeeks:       []EmptyProgramWeekResponse{},
		SessionsThisWeek: sessionsThisWeek,
		EmptyWeekCheck: EmptyWeekCheckResponse{
			DayOfWeek: settings.EmptyWeekDayOfWeek,
			Hour:      settings.EmptyWeekHour,
			Minute:    settings.EmptyWeekMinute,
			Reached:   !coachNow.Before(checkMoment),
			WeekStart: nextMonday.Format(time.DateOnly),
		},
	}

	for _, row := range pending {
		response.PendingFeedback = append(response.PendingFeedback, PendingFeedbackResponse{
			SessionID:     row.SessionID.String(),
			UserID:        row.UserID.String(),
			UserFirstname: row.UserFirstname,
			UserLastname:  row.UserLastname,
			SessionName:   row.SessionName,
			SessionDate:   row.SessionDate.Time.UTC().Format(time.RFC3339),
			Activity:      row.Activity,
			Notes:         row.Notes,
		})
	}

	if !response.EmptyWeekCheck.Reached {
		return c.Status(fiber.StatusOK).JSON(response)
	}

	emptyWeeks, err := h.queries.GetCoachEmptyProgramWeeks(c.Context(), db.GetCoachEmptyProgramWeeksParams{
		CoachID:   coachUUID,
		WeekStart: pgtype.Date{Time: nextMonday, Valid: true},
	})
	if err != nil {
		slog.Error("failed to retrieve empty program weeks", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve TODO list"})
	}

	for _, row := range emptyWeeks {
		response.EmptyWeeks = append(response.EmptyWeeks, EmptyProgramWeekResponse{
			ProgramID:     row.ProgramID.String(),
			ProgramName:   row.ProgramName,
			UserID:        row.UserID.String(),
			UserFirstname: row.UserFirstname,
			UserLastname:  row.UserLastname,
			WeekNumber:    row.WeekNumber,
			WeekStart:     nextMonday.Format(time.DateOnly),
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

func (h *CoachTodoHandler) todoSettings(c fiber.Ctx, coachUUID pgtype.UUID) (CoachTodoSettingsResponse, error) {
	settings, err := h.queries.GetCoachTodoSettings(c.Context(), coachUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return defaultTodoSettings(), nil
		}
		slog.Error("failed to retrieve TODO settings", "coach_id", coachUUID.String(), "error", err)
		return CoachTodoSettingsResponse{}, err
	}
	return todoSettingsToResponse(settings), nil
}

// GetCoachTodoSettings godoc
// @Summary Get my TODO settings
// @Description Retrieve when in the week the authenticated coach wants the programs with an unprogrammed next week to appear in their TODO. A coach who never saved any is answered the defaults, Friday at 21:00.
// @Tags Coaching
// @Produce json
// @Security BearerAuth
// @Success 200 {object} CoachTodoSettingsResponse "The settings"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/coach/todo-settings [get]
func (h *CoachTodoHandler) GetCoachTodoSettings(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	settings, err := h.todoSettings(c, coachUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve TODO settings"})
	}

	return c.Status(fiber.StatusOK).JSON(settings)
}

// SetCoachTodoSettings godoc
// @Summary Set my TODO settings
// @Description Choose when in the week the programs whose next calendar week holds no session appear in the TODO. empty_week_day_of_week is 0 = Monday to 6 = Sunday and the time is read on the coach's own clock.
// @Tags Coaching
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CoachTodoSettingsRequest true "TODO settings"
// @Success 200 {object} CoachTodoSettingsResponse "The saved settings"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/coach/todo-settings [put]
func (h *CoachTodoHandler) SetCoachTodoSettings(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var req CoachTodoSettingsRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if err := validateTodoSettings(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	settings, err := h.queries.UpsertCoachTodoSettings(c.Context(), db.UpsertCoachTodoSettingsParams{
		CoachID:            coachUUID,
		EmptyWeekDayOfWeek: req.EmptyWeekDayOfWeek,
		EmptyWeekHour:      req.EmptyWeekHour,
		EmptyWeekMinute:    req.EmptyWeekMinute,
	})
	if err != nil {
		slog.Error("failed to save TODO settings", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save TODO settings"})
	}

	return c.Status(fiber.StatusOK).JSON(todoSettingsToResponse(settings))
}
