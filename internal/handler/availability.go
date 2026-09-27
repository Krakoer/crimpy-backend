package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	daysInWeek = 7
	// A label carries what the athlete is doing, not a paragraph about it, and
	// "when" and "where" are of the same size. The old single note per day was
	// allowed 2000 characters because it was the only place anything could be
	// said; there are now as many lines as the athlete wants.
	maxActivityTextLength = 200
	// An unbounded list still needs a ceiling, or one athlete decides how big
	// every coach's week response is.
	maxActivitiesPerDay = 20
	// The same reasoning one level up. A listing with no window answers with
	// every week the athlete ever declared, which is what app builds that
	// predate the window read, so the count has to stay finite without any
	// client asking it to.
	//
	// Ten years of weeks sits far above any declaration history that exists:
	// an athlete declaring every week for two years is about a hundred of
	// them. This is a backstop against a pathological answer, not a page size,
	// and it is set where it truncates nobody real.
	//
	// Exported because it is part of what the endpoint promises, and because
	// the tests that pin it live outside this package.
	MaxAvailabilityWeeks = 520
	// What a truncated listing says so the caller is not left guessing. The
	// answer is a bare JSON array that old clients parse, so the fact cannot
	// ride in the body without breaking them, and a short answer with nothing
	// on it reads exactly like an athlete who declared fewer weeks.
	//
	// Exported because the CORS middleware has to expose exactly this header
	// for a browser to be allowed to read it, and a second spelling of it over
	// there would drift without anything failing.
	AvailabilityTruncatedHeader = "X-Availability-Weeks-Truncated"
)

type AvailabilityHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewAvailabilityHandler(queries *db.Queries, pool *pgxpool.Pool) *AvailabilityHandler {
	return &AvailabilityHandler{queries: queries, pool: pool}
}

// DayActivityRequest is one thing the athlete plans to do on one day. Only the
// label is required: a duration the athlete cannot guess, or a place they have
// not picked, are left out rather than invented.
type DayActivityRequest struct {
	Label           string  `json:"label"`
	DurationMinutes *int32  `json:"duration_minutes"`
	When            *string `json:"when"`
	Where           *string `json:"where"`
}

type DayAvailabilityRequest struct {
	DayOfWeek int32 `json:"day_of_week"`
	// Required on every day. Send an empty array for a day with nothing planned.
	Activities *[]DayActivityRequest `json:"activities"`
}

type WeekAvailabilityRequest struct {
	Days []DayAvailabilityRequest `json:"days"`
}

type DayActivityResponse struct {
	Label           string  `json:"label"`
	DurationMinutes *int32  `json:"duration_minutes,omitempty"`
	When            *string `json:"when,omitempty"`
	Where           *string `json:"where,omitempty"`
}

type DayAvailabilityResponse struct {
	DayOfWeek int32 `json:"day_of_week"`
	// Always present and never null. An empty list is a day with nothing planned.
	Activities []DayActivityResponse `json:"activities"`
}

type WeekAvailabilityResponse struct {
	UserID    string `json:"user_id"`
	WeekStart string `json:"week_start"`
	UpdatedAt string `json:"updated_at"`
	// All seven days, Monday first, whether or not anything is planned on them.
	Days []DayAvailabilityResponse `json:"days"`
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

func dayActivityToResponse(a db.CoacheeDayActivity) DayActivityResponse {
	resp := DayActivityResponse{Label: a.Label}
	if a.DurationMinutes.Valid {
		v := a.DurationMinutes.Int32
		resp.DurationMinutes = &v
	}
	if a.WhenText.Valid {
		resp.When = &a.WhenText.String
	}
	if a.WhereText.Valid {
		resp.Where = &a.WhereText.String
	}
	return resp
}

// emptyWeekDays is the shape every week answers in: seven days, Monday first,
// each holding an empty activity list until the rows fill it in.
//
// The list is built rather than left nil so it marshals as [] and never null,
// which is what lets a client iterate a day without a guard. A week only
// reaches this function because its declaration row exists, so a week present
// in a response is a declared week whatever its days hold.
func emptyWeekDays() []DayAvailabilityResponse {
	days := make([]DayAvailabilityResponse, daysInWeek)
	for day := range days {
		days[day] = DayAvailabilityResponse{
			DayOfWeek:  int32(day),
			Activities: []DayActivityResponse{},
		}
	}
	return days
}

func declarationToResponse(d db.CoacheeWeekDeclaration) WeekAvailabilityResponse {
	return WeekAvailabilityResponse{
		UserID:    d.UserID.String(),
		WeekStart: d.WeekStart.Time.Format(time.DateOnly),
		UpdatedAt: d.UpdatedAt.Time.UTC().Format(time.RFC3339),
		Days:      emptyWeekDays(),
	}
}

// availabilityToWeeks pairs each declared week with the activities hanging off
// it. A declaration with no activity at all still comes back, which is the
// whole reason it is a row of its own: it is a week the athlete answered by
// saying they have nothing planned.
//
// The activities must already be ordered by week_start, day_of_week then
// position, which every query returning them does.
func availabilityToWeeks(declarations []db.CoacheeWeekDeclaration, activities []db.CoacheeDayActivity) []WeekAvailabilityResponse {
	weeks := make([]WeekAvailabilityResponse, 0, len(declarations))
	weekByDeclaration := make(map[string]int, len(declarations))
	for _, declaration := range declarations {
		weekByDeclaration[declaration.ID.String()] = len(weeks)
		weeks = append(weeks, declarationToResponse(declaration))
	}
	for _, activity := range activities {
		index, ok := weekByDeclaration[activity.DeclarationID.String()]
		if !ok || activity.DayOfWeek < 0 || activity.DayOfWeek >= daysInWeek {
			continue
		}
		day := &weeks[index].Days[activity.DayOfWeek]
		day.Activities = append(day.Activities, dayActivityToResponse(activity))
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
	return parseWeekMonday("week_start", raw)
}

// parseWeekMonday is parseWeekStart named after whichever field carried the
// date, so a rejected window bound says which end was wrong.
func parseWeekMonday(field, raw string) (pgtype.Date, error) {
	t, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return pgtype.Date{}, fmt.Errorf("%s must be in YYYY-MM-DD format", field)
	}
	if t.Weekday() != time.Monday {
		return pgtype.Date{}, fmt.Errorf("%s must be a Monday", field)
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

// availabilityWindow is the range of calendar weeks a listing answers for, both
// ends inclusive and both optional.
//
// An absent end is unbounded on that side, and an absent window is every week
// the athlete ever declared, which is what the endpoints answered before the
// window existed. A client that knows nothing about it therefore keeps reading
// exactly what it read before.
type availabilityWindow struct {
	from pgtype.Date
	to   pgtype.Date
}

// parseAvailabilityWindow reads the window off the query string. The bounds are
// Mondays, like every other week_start in this API: a range keyed to a Thursday
// would take in half of two weeks and read as neither.
func parseAvailabilityWindow(c fiber.Ctx) (availabilityWindow, error) {
	var window availabilityWindow
	if raw := strings.TrimSpace(c.Query("from", "")); raw != "" {
		parsed, err := parseWeekMonday("from", raw)
		if err != nil {
			return availabilityWindow{}, err
		}
		window.from = parsed
	}
	if raw := strings.TrimSpace(c.Query("to", "")); raw != "" {
		parsed, err := parseWeekMonday("to", raw)
		if err != nil {
			return availabilityWindow{}, err
		}
		window.to = parsed
	}
	// Refused rather than answered with an empty list: an inverted range is a
	// caller that built its bounds wrong, and a silent [] reads on the screen
	// as an athlete who declared nothing.
	if window.from.Valid && window.to.Valid && window.to.Time.Before(window.from.Time) {
		return availabilityWindow{}, errors.New("to must not be before from")
	}
	return window, nil
}

// validateWeekAvailability demands the whole week rather than the days that
// changed: the week is one answer, and a partial write would leave the days it
// left out reading as planned-nothing without the athlete having said so.
//
// It does not demand any activity at all. A week where every day is empty is
// an athlete saying they have nothing on, which the declaration row records
// just as loudly as a full week.
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
		// The field is a pointer so an absent list is told apart from an empty
		// one. They mean opposite things: an empty list is the athlete saying
		// nothing is on that day, while an absent one is a client that does not
		// know about activities at all. Read as empty, the second would answer
		// 200 and wipe the week an installed older app was trying to write.
		if day.Activities == nil {
			return errors.New("every day must carry an activities list, empty when nothing is planned")
		}
		if len(*day.Activities) > maxActivitiesPerDay {
			return fmt.Errorf("a day carries at most %d activities", maxActivitiesPerDay)
		}
		for _, activity := range *day.Activities {
			if err := validateDayActivity(activity); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateDayActivity measures the trimmed value rather than what was sent,
// since trimming is what reaches the column: a label of exactly the limit with
// a trailing space would otherwise be refused for a length it does not store.
func validateDayActivity(activity DayActivityRequest) error {
	label := strings.TrimSpace(activity.Label)
	if label == "" {
		return errors.New("every activity needs a label")
	}
	if err := checkActivityTextLength("label", label); err != nil {
		return err
	}
	if activity.DurationMinutes != nil && *activity.DurationMinutes <= 0 {
		return errors.New("duration_minutes must be greater than 0")
	}
	if err := checkActivityTextLength("when", trimmedValue(activity.When)); err != nil {
		return err
	}
	return checkActivityTextLength("where", trimmedValue(activity.Where))
}

func trimmedValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func checkActivityTextLength(field, value string) error {
	if utf8.RuneCountInString(value) <= maxActivityTextLength {
		return nil
	}
	return fmt.Errorf("%s must be at most %d characters", field, maxActivityTextLength)
}

// trimmedText drops a field the athlete left blank rather than storing an empty
// string, so a client can tell "not said" from "said nothing" without comparing
// against "".
func trimmedText(value *string) pgtype.Text {
	trimmed := trimmedValue(value)
	if trimmed == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: trimmed, Valid: true}
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
// @Summary Get my declared weeks
// @Description Retrieve the calendar weeks the authenticated user has declared, each carrying the seven days and the activities planned on them. from and to bound the answer to a range of calendar weeks, both Mondays and both inclusive. Either may be left out for an unbounded end, and leaving both out returns every week ever declared. A week carrying up to 140 activities makes the unbounded answer large, so a screen showing one week at a time should ask for that week. At most 520 weeks come back whatever the window; when older weeks were dropped the response carries X-Availability-Weeks-Truncated: true, and the weeks kept are the most recent ones. The set of declared weeks is not a window question: read /api/user/availability/declared-weeks for that.
// @Tags Availability
// @Produce json
// @Security BearerAuth
// @Param from query string false "First calendar week to return, Monday, YYYY-MM-DD"
// @Param to query string false "Last calendar week to return, Monday, YYYY-MM-DD"
// @Success 200 {array} WeekAvailabilityResponse "Declared weeks"
// @Header 200 {string} X-Availability-Weeks-Truncated "true when older weeks were dropped at the 520 week ceiling, absent otherwise"
// @Failure 400 {object} map[string]string "Invalid window"
// @Failure 401 {object} map[string]string "Invalid user ID"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/user/availability [get]
func (h *AvailabilityHandler) GetMyAvailability(c fiber.Ctx) error {
	userUUID, ok := requireCallerUUID(c)
	if !ok {
		return nil
	}

	window, err := parseAvailabilityWindow(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	weeks, truncated, err := h.loadWeeks(c, userUUID, window)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve availability"})
	}

	return respondWithWeeks(c, weeks, truncated)
}

// GetMyDeclaredWeeks godoc
// @Summary List the Mondays I have declared
// @Description Every calendar week the authenticated user has declared, as Mondays in YYYY-MM-DD, oldest first, with no activities and no window. This is what the athlete app plans its declaration reminders from: a nudge is dropped for a week that was already answered, so the planner needs the whole set and not the window a screen happens to be showing. Carrying no activities keeps it small enough to answer in full.
// @Tags Availability
// @Produce json
// @Security BearerAuth
// @Success 200 {array} string "Declared week starts"
// @Failure 401 {object} map[string]string "Invalid user ID"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/user/availability/declared-weeks [get]
func (h *AvailabilityHandler) GetMyDeclaredWeeks(c fiber.Ctx) error {
	userUUID, ok := requireCallerUUID(c)
	if !ok {
		return nil
	}

	weekStarts, err := h.queries.ListCoacheeDeclaredWeekStarts(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve declared weeks", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve availability"})
	}

	// Built rather than left nil so it marshals as [] and never null: an
	// athlete who has declared nothing is an empty set, which the planner
	// iterates without a guard.
	mondays := make([]string, 0, len(weekStarts))
	for _, weekStart := range weekStarts {
		mondays = append(mondays, weekStart.Time.Format(time.DateOnly))
	}

	return c.Status(fiber.StatusOK).JSON(mondays)
}

// loadWeeks reads the weeks a coachee declared inside the window, with what
// they planned in them, and says whether the ceiling cut the answer short. The
// declarations are read on their own rather than derived from the activity
// rows, so a week holding no activity is still in the answer.
//
// The activities are read for the weeks that survived the ceiling rather than
// for the window asked for, so a week that is in the answer carries all of its
// activities and a week the ceiling dropped carries none of the megabytes it
// would have contributed.
func (h *AvailabilityHandler) loadWeeks(c fiber.Ctx, userUUID pgtype.UUID, window availabilityWindow) ([]WeekAvailabilityResponse, bool, error) {
	// One row past the ceiling, which is what tells a whole answer from a cut
	// one without paying for a second counting query.
	declarations, err := h.queries.ListCoacheeWeekDeclarations(c.Context(), db.ListCoacheeWeekDeclarationsParams{
		UserID:   userUUID,
		FromWeek: window.from,
		ToWeek:   window.to,
		RowLimit: MaxAvailabilityWeeks + 1,
	})
	if err != nil {
		slog.Error("failed to retrieve availability", "user_id", userUUID.String(), "error", err)
		return nil, false, err
	}
	truncated := len(declarations) > MaxAvailabilityWeeks
	if truncated {
		// The oldest go, since the rows arrive oldest first and recent weeks
		// are what every caller renders.
		declarations = declarations[len(declarations)-MaxAvailabilityWeeks:]
		slog.Warn("availability listing truncated", "user_id", userUUID.String(), "max_weeks", MaxAvailabilityWeeks)
	}
	if len(declarations) == 0 {
		return []WeekAvailabilityResponse{}, false, nil
	}
	activities, err := h.queries.ListCoacheeDayActivities(c.Context(), db.ListCoacheeDayActivitiesParams{
		UserID:   userUUID,
		FromWeek: declarations[0].WeekStart,
		ToWeek:   window.to,
	})
	if err != nil {
		slog.Error("failed to retrieve availability activities", "user_id", userUUID.String(), "error", err)
		return nil, false, err
	}
	return availabilityToWeeks(declarations, activities), truncated, nil
}

// respondWithWeeks answers a listing, flagging the answer when the ceiling cut
// it. The header is set only on a cut answer, so its absence is a caller's
// proof that it read the athlete's whole history rather than the start of it.
func respondWithWeeks(c fiber.Ctx, weeks []WeekAvailabilityResponse, truncated bool) error {
	if truncated {
		c.Set(AvailabilityTruncatedHeader, "true")
	}
	return c.Status(fiber.StatusOK).JSON(weeks)
}

// insertWeekActivities sends the whole week's activities as one batch. The
// first error is kept and the results are drained either way: leaving a batch
// unread poisons the connection for whatever runs on it next, including the
// rollback this error is about to trigger.
func insertWeekActivities(c fiber.Ctx, qtx *db.Queries, inserts []db.InsertCoacheeDayActivityParams) error {
	if len(inserts) == 0 {
		return nil
	}
	results := qtx.InsertCoacheeDayActivity(c.Context(), inserts)
	var firstErr error
	results.Exec(func(_ int, err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	})
	if closeErr := results.Close(); closeErr != nil && firstErr == nil {
		firstErr = closeErr
	}
	return firstErr
}

// UpsertMyWeekAvailability godoc
// @Summary Declare my schedule for a calendar week
// @Description Replace the authenticated user's schedule for one calendar week. The body must carry all seven days, day_of_week 0 = Monday to 6 = Sunday. Every day must carry an activities array, empty when nothing is planned on it; leaving the key out is refused. A day's activities come back in the order they were sent. A week where every day is empty is still a declared week.
// @Tags Availability
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param week_start path string true "Monday of the week, YYYY-MM-DD"
// @Param request body WeekAvailabilityRequest true "The seven days of the week"
// @Success 200 {object} WeekAvailabilityResponse "The declared week"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 401 {object} map[string]string "Invalid user ID"
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

	tx, err := h.pool.Begin(c.Context())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save availability"})
	}
	defer tx.Rollback(c.Context())

	qtx := h.queries.WithTx(tx)
	// The declaration first, for two reasons. It is what says the week was
	// answered, so it has to exist even when every day below turns out to be
	// empty; and it is one row per week, so two concurrent writes of the same
	// week serialise on it here rather than racing over the activities under
	// it.
	declaration, err := qtx.UpsertCoacheeWeekDeclaration(c.Context(), db.UpsertCoacheeWeekDeclarationParams{
		UserID:    userUUID,
		WeekStart: weekStart,
	})
	if err != nil {
		slog.Error("failed to declare availability week", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save availability"})
	}

	// The week is one answer, so what it held before is cleared wholesale
	// rather than reconciled: an activity the athlete removed has no key to
	// match an update against.
	if err := qtx.DeleteCoacheeWeekActivities(c.Context(), declaration.ID); err != nil {
		slog.Error("failed to clear availability activities", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save availability"})
	}

	inserts := []db.InsertCoacheeDayActivityParams{}
	for _, day := range req.Days {
		for position, activity := range *day.Activities {
			params := db.InsertCoacheeDayActivityParams{
				DeclarationID: declaration.ID,
				DayOfWeek:     day.DayOfWeek,
				Position:      int32(position),
				Label:         strings.TrimSpace(activity.Label),
				WhenText:      trimmedText(activity.When),
				WhereText:     trimmedText(activity.Where),
			}
			if activity.DurationMinutes != nil {
				params.DurationMinutes = pgtype.Int4{Int32: *activity.DurationMinutes, Valid: true}
			}
			inserts = append(inserts, params)
		}
	}
	if err := insertWeekActivities(c, qtx, inserts); err != nil {
		slog.Error("failed to save availability activities", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save availability"})
	}

	if err := tx.Commit(c.Context()); err != nil {
		slog.Error("failed to commit availability", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize availability"})
	}

	return h.respondWithWeek(c, userUUID, weekStart)
}

func (h *AvailabilityHandler) respondWithWeek(c fiber.Ctx, userUUID pgtype.UUID, weekStart pgtype.Date) error {
	declaration, err := h.queries.GetCoacheeWeekDeclaration(c.Context(), db.GetCoacheeWeekDeclarationParams{
		UserID:    userUUID,
		WeekStart: weekStart,
	})
	if err != nil {
		// The declaration was committed a moment ago, so a missing row is a
		// broken invariant rather than a week the client should handle.
		slog.Error("failed to reload availability", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve availability"})
	}
	activities, err := h.queries.ListCoacheeWeekDayActivities(c.Context(), declaration.ID)
	if err != nil {
		slog.Error("failed to reload availability activities", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve availability"})
	}
	return c.Status(fiber.StatusOK).JSON(availabilityToWeeks([]db.CoacheeWeekDeclaration{declaration}, activities)[0])
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
// @Summary Get a client's declared weeks
// @Description Retrieve the calendar weeks a user enrolled with the authenticated coach has declared, each carrying the seven days and the activities planned on them. from and to bound the answer to a range of calendar weeks, both Mondays and both inclusive. Either may be left out for an unbounded end, and leaving both out returns every week ever declared. A program page should ask for the weeks the program covers rather than the athlete's whole history. At most 520 weeks come back whatever the window; when older weeks were dropped the response carries X-Availability-Weeks-Truncated: true, and the weeks kept are the most recent ones.
// @Tags Availability
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param from query string false "First calendar week to return, Monday, YYYY-MM-DD"
// @Param to query string false "Last calendar week to return, Monday, YYYY-MM-DD"
// @Success 200 {array} WeekAvailabilityResponse "Declared weeks"
// @Header 200 {string} X-Availability-Weeks-Truncated "true when older weeks were dropped at the 520 week ceiling, absent otherwise"
// @Failure 400 {object} map[string]string "Invalid client ID or window"
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

	window, err := parseAvailabilityWindow(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	weeks, truncated, err := h.loadWeeks(c, clientUUID, window)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve availability"})
	}

	return respondWithWeeks(c, weeks, truncated)
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
