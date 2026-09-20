package handler

import (
	"crimpy/backend/internal/db"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultTrainingLoadWeeks = 12
	maxTrainingLoadWeeks     = 52

	// How many weeks the chronic load averages over, the current one included.
	chronicLoadWeeks = 3

	// Weeks read before the first one returned, so the earliest week shown has
	// the same full chronic window and the same week over week comparison as
	// every week after it.
	trainingLoadLeadInWeeks = chronicLoadWeeks - 1

	secondsPerMinute = 60
)

type CoachTrainingLoadHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewCoachTrainingLoadHandler(queries *db.Queries, pool *pgxpool.Pool) *CoachTrainingLoadHandler {
	return &CoachTrainingLoadHandler{queries: queries, pool: pool}
}

// WeeklyTrainingLoadResponse is one calendar week of the coach's load view.
//
// Every load figure is nullable on purpose. A week with no session at all has a
// known load of zero, while a week the athlete trained and rated nothing has no
// knowable mean RPE, and answering zero for it would claim the training was
// effortless. The two cases are told apart by session_count.
type WeeklyTrainingLoadResponse struct {
	// WeekStart is the Monday the week opens on, read on the caller's clock.
	WeekStart string `json:"week_start"`
	// WeekNumber and ProgramName name the program week this calendar week is,
	// when a program of this coach covers it. Both null otherwise.
	WeekNumber  *int32  `json:"week_number"`
	ProgramName *string `json:"program_name"`

	SessionCount    int32 `json:"session_count"`
	TotalMinutes    int32 `json:"total_minutes"`
	ClimbingMinutes int32 `json:"climbing_minutes"`
	StrengthMinutes int32 `json:"strength_minutes"`
	// RatedSessions is how many of the week's sessions carry an RPE, so the
	// coach can see how much of the week the mean actually speaks for.
	RatedSessions int32 `json:"rated_sessions"`
	// FailedSessions counts the sessions marked ECHEC. They are deliberately
	// outside mean_rpe: see the note on the endpoint.
	FailedSessions int32 `json:"failed_sessions"`

	MeanRpe   *float64 `json:"mean_rpe"`
	AcuteLoad *float64 `json:"acute_load"`
	// ChronicLoad averages the acute load of this week and the weeks before it,
	// ChronicWeeks of them in all, never more than three and never reaching
	// before the athlete's first recorded session.
	ChronicLoad *float64 `json:"chronic_load"`
	// ChronicWeeks is how many weeks the mean beside it actually rested on, so
	// it is not always a count of how much history exists. A week inside the
	// window whose own load is unknown is skipped rather than counted as zero,
	// the current week included, so a 2 can mean "only two weeks of history" or
	// "three weeks, one of them unrated". It is 0 exactly when ChronicLoad is
	// null, which is a week with no baseline at all rather than a baseline of
	// nothing.
	ChronicWeeks int32 `json:"chronic_weeks"`
	// AcuteChronicRatio is acute over chronic, null when there is no chronic
	// baseline to divide by yet.
	AcuteChronicRatio *float64 `json:"acute_chronic_ratio"`
	// LoadChangePercent is this week's acute load as a percentage of last
	// week's, null when last week's is unknown or zero.
	LoadChangePercent *float64 `json:"load_change_percent"`
}

type TrainingLoadResponse struct {
	Weeks []WeeklyTrainingLoadResponse `json:"weeks"`
}

// weeklyLoad is one week while the series is still being derived, before the
// lead-in weeks are dropped and the rows are shaped for the wire.
type weeklyLoad struct {
	weekStart time.Time
	row       db.GetCoacheeWeeklyTrainingLoadRow

	totalMinutes    int32
	climbingMinutes int32
	strengthMinutes int32

	meanRpe   *float64
	acuteLoad *float64

	chronicLoad       *float64
	chronicWeeks      int32
	acuteChronicRatio *float64
	loadChangePercent *float64
}

// minutesOf converts a week's stored seconds to the minutes every figure in
// this view is expressed in, rounded once so the acute load a coach reads is
// exactly the mean RPE times the minutes shown beside it.
func minutesOf(seconds int64) int32 {
	return int32(math.Round(float64(seconds) / secondsPerMinute))
}

// deriveWeek fills in the figures that come from one week alone.
//
// mean_rpe is the mean over the sessions the athlete actually rated. A session
// left unrated is not a zero and is not counted, since counting it would pull
// the mean down and report the week as lighter than it was. A session marked
// ECHEC is not counted either: ECHEC names an outcome rather than a point on
// the 5 to 10 scale, so it has no value to average, and inventing one would be
// indistinguishable in the answer from a rating the athlete gave. It is
// reported on its own as failed_sessions instead.
func deriveWeek(row db.GetCoacheeWeeklyTrainingLoadRow) weeklyLoad {
	week := weeklyLoad{
		weekStart:       row.WeekStart.Time,
		row:             row,
		totalMinutes:    minutesOf(row.TotalSeconds),
		climbingMinutes: minutesOf(row.ClimbingSeconds),
		strengthMinutes: minutesOf(row.StrengthSeconds),
	}

	if row.RatedSessions > 0 {
		mean := float64(row.RpeSum) / float64(row.RatedSessions)
		week.meanRpe = &mean
	}

	switch {
	case row.SessionCount == 0:
		// Nothing was trained, so the load is a known zero rather than unknown.
		// This is what keeps a rest week in the chronic mean.
		zero := 0.0
		week.acuteLoad = &zero
	case week.totalMinutes == 0:
		// Sessions were recorded but none of them carries a duration. duration
		// is NOT NULL DEFAULT 0, so a client that omits it writes a zero that
		// means "not recorded" rather than "no time spent". Multiplying by it
		// would hand back a load of zero for a week that was trained, which is
		// the same misreport an unrated session would make, in the same
		// direction. Left unknown for the same reason.
	case week.meanRpe != nil:
		load := *week.meanRpe * float64(week.totalMinutes)
		week.acuteLoad = &load
	}

	return week
}

// deriveSeries fills in the figures that need the weeks before, in order.
//
// The chronic load is an expanding mean rather than a flat three week one for
// the weeks at the start of the athlete's history: it averages the current week
// and up to the two before it, counting only weeks from firstHistoryWeek on. An
// athlete who recorded their first session three weeks ago did not rest for the
// weeks before that, and averaging that silence in would show their first weeks
// as a far sharper ramp than they were. So the first week of a history has a
// chronic load equal to its own acute load, and a ratio of exactly 1.00, which
// chronic_weeks marks as resting on a single week.
//
// A week whose acute load is unknown, meaning it holds sessions but not one
// rating, is left out of the mean rather than counted as zero, for the same
// reason an unrated session is left out of mean_rpe.
//
// An athlete with nothing recorded at all has no history for any week to sit
// in, so no week gets a baseline: a chronic load of zero drawn flat across the
// view would read as a measurement rather than as the absence of one.
func deriveSeries(weeks []weeklyLoad, firstHistoryWeek *time.Time) {
	if firstHistoryWeek == nil {
		return
	}

	for i := range weeks {
		if weeks[i].weekStart.Before(*firstHistoryWeek) {
			continue
		}

		sum := 0.0
		counted := int32(0)
		for back := 0; back < chronicLoadWeeks; back++ {
			j := i - back
			if j < 0 {
				break
			}
			if weeks[j].weekStart.Before(*firstHistoryWeek) {
				break
			}
			if weeks[j].acuteLoad == nil {
				continue
			}
			sum += *weeks[j].acuteLoad
			counted++
		}

		if counted > 0 {
			chronic := sum / float64(counted)
			weeks[i].chronicLoad = &chronic
			weeks[i].chronicWeeks = counted
		}

		if weeks[i].acuteLoad != nil && weeks[i].chronicLoad != nil && *weeks[i].chronicLoad > 0 {
			ratio := *weeks[i].acuteLoad / *weeks[i].chronicLoad
			weeks[i].acuteChronicRatio = &ratio
		}

		if i == 0 {
			continue
		}
		previous := weeks[i-1].acuteLoad
		if weeks[i].acuteLoad != nil && previous != nil && *previous > 0 {
			change := *weeks[i].acuteLoad / *previous * 100
			weeks[i].loadChangePercent = &change
		}
	}
}

func (w weeklyLoad) toResponse() WeeklyTrainingLoadResponse {
	return WeeklyTrainingLoadResponse{
		WeekStart:         w.weekStart.Format(time.DateOnly),
		SessionCount:      w.row.SessionCount,
		TotalMinutes:      w.totalMinutes,
		ClimbingMinutes:   w.climbingMinutes,
		StrengthMinutes:   w.strengthMinutes,
		RatedSessions:     w.row.RatedSessions,
		FailedSessions:    w.row.FailedSessions,
		MeanRpe:           w.meanRpe,
		AcuteLoad:         w.acuteLoad,
		ChronicLoad:       w.chronicLoad,
		ChronicWeeks:      w.chronicWeeks,
		AcuteChronicRatio: w.acuteChronicRatio,
		LoadChangePercent: w.loadChangePercent,
	}
}

// parseTrainingLoadWeeks reads how many weeks the caller wants shown.
func parseTrainingLoadWeeks(raw string) (int, error) {
	if raw == "" {
		return defaultTrainingLoadWeeks, nil
	}
	weeks, err := strconv.Atoi(raw)
	if err != nil || weeks < 1 || weeks > maxTrainingLoadWeeks {
		return 0, fmt.Errorf("weeks must be a whole number between 1 and %d", maxTrainingLoadWeeks)
	}
	return weeks, nil
}

// GetClientTrainingLoad godoc
// @Summary Get a client's weekly training load
// @Description The weekly training load series for a coachee enrolled with the authenticated coach, oldest week first and ending with the week being trained now. Weeks are cut on Monday in the caller's own time, which is what tz_offset_minutes carries, and a week holding no session is returned with zeros rather than skipped. Durations are reported in minutes, summed from the seconds stored on each session. mean_rpe averages only the sessions the athlete rated: an unrated session is left out rather than counted as zero, and a session marked ECHEC is left out too and reported separately as failed_sessions, because ECHEC is an outcome rather than a point on the 5 to 10 scale. acute_load is mean_rpe times total_minutes, zero for a week with no session at all and null for a week that holds sessions but no rating or no recorded duration, since the effort is then simply not known. chronic_load averages the acute load of this week and up to the two before it, never reaching before the athlete's first recorded session, skipping any week whose own load is unknown, and chronic_weeks says how many weeks it actually rested on, 0 meaning no baseline at all. acute_chronic_ratio and load_change_percent are null wherever there is no baseline to divide by. The minutes of each bucket are rounded from their own second totals, so the climbing and strength figures can differ from the total by a minute on sub minute sessions. tz_offset_minutes is applied uniformly to every week in the window, so a window spanning a daylight saving change is an hour out on the far side of it, see Krakoer/crimpy#123. The interpretation bands the coach reads these against are guidance held by the portal, not a judgement this endpoint makes.
// @Tags Coaching
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param weeks query int false "How many weeks to return, 1 to 52, defaults to 12"
// @Param tz_offset_minutes query int false "Caller's offset east of UTC in minutes, defaults to 0"
// @Success 200 {object} TrainingLoadResponse "The weekly series"
// @Failure 400 {object} map[string]string "Invalid parameters"
// @Failure 403 {object} map[string]string "Not a validated coach, or client not enrolled"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/coach/clients/{user_id}/training-load [get]
func (h *CoachTrainingLoadHandler) GetClientTrainingLoad(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	clientUUID, ok := verifyClientEnrolled(c, h.queries, coachUUID, c.Params("user_id"))
	if !ok {
		return nil
	}

	offset, err := parseTimezoneOffset(c.Query("tz_offset_minutes", ""))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	weeksWanted, err := parseTrainingLoadWeeks(c.Query("weeks", ""))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// The clock is shifted rather than located, the way coach_todo.go does it,
	// so every Monday below is the caller's own without the server needing to
	// know their timezone name.
	//
	// The offset is the caller's at request time and is applied to every week
	// in the window, not only the current one. A window spanning a daylight
	// saving change is therefore an hour out on the far side of it, which moves
	// a session recorded within that hour of a Monday midnight into the
	// neighbouring week. Named rather than fixed here because fixing it means
	// taking an IANA zone name instead of an offset, which is a change to the
	// convention coach_todo.go also carries: Krakoer/crimpy#123.
	lastMonday := mondayOfWeek(time.Now().UTC().Add(offset))
	firstShownMonday := lastMonday.AddDate(0, 0, -daysInWeek*(weeksWanted-1))
	firstReadMonday := firstShownMonday.AddDate(0, 0, -daysInWeek*trainingLoadLeadInWeeks)

	// Back out of the shifted clock to name the real instants the window runs
	// between, since the sessions being read are stored in UTC.
	windowStart := firstReadMonday.Add(-offset)
	windowEnd := lastMonday.AddDate(0, 0, daysInWeek).Add(-offset)

	rows, err := h.queries.GetCoacheeWeeklyTrainingLoad(c.Context(), db.GetCoacheeWeeklyTrainingLoadParams{
		FirstWeekStart:  pgtype.Date{Time: firstReadMonday, Valid: true},
		LastWeekStart:   pgtype.Date{Time: lastMonday, Valid: true},
		TzOffsetMinutes: int32(offset / time.Minute),
		UserID:          clientUUID,
		WindowStart:     pgtype.Timestamptz{Time: windowStart, Valid: true},
		WindowEnd:       pgtype.Timestamptz{Time: windowEnd, Valid: true},
	})
	if err != nil {
		slog.Error("failed to retrieve weekly training load", "coach_id", coachUUID.String(), "client_id", clientUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training load"})
	}

	firstSession, err := h.queries.GetCoacheeFirstSessionInstant(c.Context(), clientUUID)
	if err != nil {
		slog.Error("failed to retrieve first session instant", "coach_id", coachUUID.String(), "client_id", clientUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training load"})
	}

	var firstHistoryWeek *time.Time
	if firstSession.Valid {
		monday := mondayOfWeek(firstSession.Time.UTC().Add(offset))
		firstHistoryWeek = &monday
	}

	weeks := make([]weeklyLoad, 0, len(rows))
	for _, row := range rows {
		weeks = append(weeks, deriveWeek(row))
	}
	deriveSeries(weeks, firstHistoryWeek)

	programWeeks, err := h.queries.GetCoacheeProgramWeekNumbers(c.Context(), db.GetCoacheeProgramWeekNumbersParams{
		UserID:         clientUUID,
		CoachID:        coachUUID,
		FirstWeekStart: pgtype.Date{Time: firstShownMonday, Valid: true},
		LastWeekStart:  pgtype.Date{Time: lastMonday, Valid: true},
	})
	if err != nil {
		slog.Error("failed to retrieve program week numbers", "coach_id", coachUUID.String(), "client_id", clientUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve training load"})
	}

	programByWeek := make(map[string]db.GetCoacheeProgramWeekNumbersRow, len(programWeeks))
	for _, row := range programWeeks {
		programByWeek[row.WeekStart.Time.Format(time.DateOnly)] = row
	}

	response := TrainingLoadResponse{Weeks: make([]WeeklyTrainingLoadResponse, 0, weeksWanted)}
	for _, week := range weeks {
		if week.weekStart.Before(firstShownMonday) {
			continue
		}
		shown := week.toResponse()
		if program, found := programByWeek[shown.WeekStart]; found {
			weekNumber := program.WeekNumber
			programName := program.ProgramName
			shown.WeekNumber = &weekNumber
			shown.ProgramName = &programName
		}
		response.Weeks = append(response.Weeks, shown)
	}

	return c.Status(fiber.StatusOK).JSON(response)
}
