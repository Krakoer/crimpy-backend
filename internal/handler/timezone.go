package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"

	// The API image is built on bare alpine, which carries no zoneinfo, so
	// time.LoadLocation below would fail there with nothing to read. Embedding
	// the database costs a little binary size and removes the dependency on
	// what the image happens to ship. It does not make every build read the
	// same rules: LoadLocation reads $ZONEINFO and /usr/share/zoneinfo first
	// and only falls back on the embedded copy, so a host that has zoneinfo,
	// which is every development machine, still resolves from its own.
	_ "time/tzdata"
)

const (
	minTimezoneOffsetMin = -12 * 60
	maxTimezoneOffsetMin = 14 * 60

	// The zone name the server itself runs in. time.LoadLocation answers it,
	// which would silently cut a caller's weeks on the host's clock.
	serverLocationName = "Local"
)

// callerClock is the clock an endpoint cuts its weeks on.
//
// A zone name is the exact answer, because it carries the daylight saving
// rules: a Monday midnight months back is then the one the caller actually
// lived through. An offset only describes their clock at the moment they asked,
// so a window reaching back across a daylight saving change is an hour out on
// the far side of it. The offset is still accepted, and is still what a client
// that predates the zone parameter sends, so it stays the fallback rather than
// being replaced.
//
// Both cases end up as a *time.Location, the offset one as a fixed zone, so the
// arithmetic below is written once.
type callerClock struct {
	zone *time.Location
	// name is the IANA zone the caller sent, empty when they sent an offset.
	// The weekly aggregate is grouped by the database, which needs the same
	// zone to cut the same weeks.
	name string
	// offset is what the caller sent as tz_offset_minutes, kept whether or not
	// a zone came with it so the query is handed the same pair the request
	// carried rather than a value invented here.
	offset time.Duration
}

// parseCallerClock reads the caller's clock off the request.
//
// Both parameters are validated whatever the other one says, so a client
// sending a malformed offset alongside a good zone still hears about it rather
// than having it quietly ignored.
func parseCallerClock(c fiber.Ctx) (callerClock, error) {
	offset, err := parseTimezoneOffset(c.Query("tz_offset_minutes", ""))
	if err != nil {
		return callerClock{}, err
	}

	name := strings.TrimSpace(c.Query("timezone", ""))
	if name == "" {
		return callerClock{zone: time.FixedZone("", int(offset/time.Second)), offset: offset}, nil
	}

	zone, err := loadCallerZone(name)
	if err != nil {
		return callerClock{}, err
	}
	return callerClock{zone: zone, name: name, offset: offset}, nil
}

// zoneName is the zone to hand a query that has to cut the same weeks, null
// when the caller only sent an offset and the query must fall back on it.
func (clock callerClock) zoneName() pgtype.Text {
	return pgtype.Text{String: clock.name, Valid: clock.name != ""}
}

// offsetMinutes is the fallback the query cuts its weeks with when no zone was
// sent. It is still passed alongside a zone, and ignored there, so the two
// always travel together and neither side has to guess what the other used.
func (clock callerClock) offsetMinutes() int32 {
	return int32(clock.offset / time.Minute)
}

// loadCallerZone resolves an IANA zone name a caller sent.
//
// It refuses the names that resolve to something other than a zone the caller
// could be in: "Local" is the server's own, and the empty name is UTC by
// accident rather than by request. Anything else LoadLocation rejects is
// refused here as a bad request, rather than reaching the database and failing
// there as a server error.
func loadCallerZone(name string) (*time.Location, error) {
	invalid := errors.New("timezone must be an IANA zone name such as Europe/Paris")
	if name == serverLocationName {
		return nil, invalid
	}
	zone, err := time.LoadLocation(name)
	if err != nil {
		return nil, invalid
	}
	return zone, nil
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
	if err != nil || minutes < minTimezoneOffsetMin || minutes > maxTimezoneOffsetMin {
		return 0, fmt.Errorf("tz_offset_minutes must be a whole number of minutes between %d and %d", minTimezoneOffsetMin, maxTimezoneOffsetMin)
	}
	return time.Duration(minutes) * time.Minute, nil
}

// mondayOfWeek is the instant the caller's week holding at opened, their own
// Monday at midnight. day_of_week is 0 = Monday everywhere in this codebase,
// while Go counts from Sunday, which is what the shift corrects.
//
// A zone that springs forward across its own midnight has a day with no
// midnight to name, and time.Date answers such a wall clock with 23:00 the day
// before, which would be the wrong day. Scanning every zone Go carries for the
// years 1990 to 2035 finds twelve Mondays with no midnight, all of them South
// American or Pacific and all between 1989 and 1998, so no window this API will
// read, which reaches at most 52 weeks back, can land on one.
func (clock callerClock) mondayOfWeek(at time.Time) time.Time {
	local := at.In(clock.zone)
	daysSinceMonday := (int(local.Weekday()) + 6) % 7
	day := local.AddDate(0, 0, -daysSinceMonday)
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, clock.zone)
}

// calendarDate is the bare day an instant falls on where it is located, held at
// UTC midnight.
//
// Week starts are dates in this API: they are handed to the database as dates,
// compared against the dates it hands back, and printed as dates. A local
// midnight is a different instant from the UTC midnight of the same day, so
// they are converted here rather than compared as the instants they came from.
func calendarDate(at time.Time) time.Time {
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
}
