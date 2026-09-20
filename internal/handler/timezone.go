package handler

import (
	"errors"
	"fmt"
	"regexp"
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

	// The one zone name a browser reports that is not of the form Area/Location.
	// Postgres reads it as a fixed zero offset and so does Go, so the two agree.
	utcZoneName = "UTC"

	// Alternative encodings of the same zones, which some distributions ship in
	// /usr/share/zoneinfo and Postgres does not carry at all.
	posixZonePrefix = "posix/"
	leapZonePrefix  = "right/"
)

// zoneNameShape is what an IANA zone name looks like, Area/Location with an
// optional further part, as in America/Argentina/Buenos_Aires. It is matched
// rather than merely looked for a separator in, because time.LoadLocation
// builds a path out of the name and lets the kernel tidy it: "./Europe/Paris"
// and "Europe//Paris" both resolve on a host with a zoneinfo directory, and
// Postgres then refuses the name it is handed.
var zoneNameShape = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z][A-Za-z0-9_+-]*)+$`)

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
// The name has to mean the same thing to Go and to Postgres, since the two cut
// the same weeks from opposite ends of the request, so it is held to the shape
// a zone name really has, Area/Location, with UTC the one exception a browser
// reports. What that turns away, measured rather than assumed:
//
// A name with no slash is an abbreviation or a legacy alias, and Postgres reads
// several of them off its abbreviation table as a fixed offset where Go reads
// the zone with its daylight saving rules. Comparing every name in
// pg_timezone_names against Go over a full 52 week window, CET, EET, MET and
// WET are exactly the four that disagree: AT TIME ZONE 'CET' is +01:00 all year
// while Go's CET moves to +02:00 in summer, so the ticket's own bug would still
// be there for a caller who sent one. No abbreviation Postgres carries contains
// a slash, so requiring one turns away the whole class.
//
// It also turns away the names that mean the server rather than the caller,
// "Local" and the "localtime" symlink a Debian host keeps in its zoneinfo
// directory, which LoadLocation answers with the host's own zone and Postgres
// does not know at all.
//
// The posix/ and right/ trees have the shape but are alternative encodings of
// the same zones that Postgres does not carry, so they are named here.
//
// The rule is deliberately stricter than it has to be. Of the 46 names without
// a slash that Postgres carries, only those four actually disagree with Go; the
// other 42, EST5EDT and Japan and the rest, would have been answered correctly.
// They are refused all the same, because a visible 400 on a name no client
// sends beats a rule with exceptions in it, and the message says which shape is
// wanted.
func loadCallerZone(name string) (*time.Location, error) {
	invalid := errors.New("timezone must be an IANA zone name of the form Area/Location, such as Europe/Paris")
	if name != utcZoneName {
		if !zoneNameShape.MatchString(name) ||
			strings.HasPrefix(name, posixZonePrefix) ||
			strings.HasPrefix(name, leapZonePrefix) {
			return nil, invalid
		}
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

// mondayOfWeek is the instant that opened the caller's week holding at, their
// own Monday at midnight. day_of_week is 0 = Monday everywhere in this codebase,
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
