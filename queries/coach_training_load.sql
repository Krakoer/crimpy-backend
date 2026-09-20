-- name: GetCoacheeWeeklyTrainingLoad :many
-- The raw weekly aggregates behind the coach's training load view, one row per
-- calendar week between the two Mondays asked for. Weeks the athlete trained
-- nothing are returned as zeros rather than skipped: dropping them shortens the
-- rolling chronic mean, which then reads as if the rest week never happened.
--
-- Weeks are cut on the coach's own Monday. Session instants are stored in UTC,
-- so they are shifted by the caller's offset before being truncated, and the
-- grid is built in that same shifted space. The shift is spelled out rather
-- than left to AT TIME ZONE because date_trunc on a bare timestamp answers the
-- same whatever the server's TimeZone setting happens to be.
--
-- Only the raw sums live here. The mean RPE, the acute and chronic loads and
-- the ratios are derived by the handler, where the rules about an unrated
-- session and a failed one can be written once and read.
--
-- Durations are stored in seconds on every session and summed as seconds, so
-- the conversion to minutes and its rounding happen in exactly one place.
WITH grid AS (
  SELECT generate_series(
    sqlc.arg('first_week_start')::date,
    sqlc.arg('last_week_start')::date,
    interval '7 days'
  )::date AS week_start
),
weekly AS (
  SELECT
    (date_trunc(
      'week',
      (s.date AT TIME ZONE 'UTC') + make_interval(mins => sqlc.arg('tz_offset_minutes')::integer)
    ))::date AS week_start,
    COUNT(*)::integer AS session_count,
    COALESCE(SUM(s.duration), 0)::bigint AS total_seconds,
    -- activity 1 is climbing; 0 hangboard and 3 workout are the strength side.
    -- 2 stretching and 4 other are neither, so they count in the total minutes
    -- and stay out of the climbing to strength ratio.
    COALESCE(SUM(s.duration) FILTER (WHERE s.activity = 1), 0)::bigint AS climbing_seconds,
    COALESCE(SUM(s.duration) FILTER (WHERE s.activity IN (0, 3)), 0)::bigint AS strength_seconds,
    -- COUNT over a nullable column skips the nulls, so this is the number of
    -- sessions the athlete actually rated, and the sum below is over those same
    -- rows. An unrated session contributes to neither, which is what keeps it
    -- from reading as a zero and dragging the mean down.
    COUNT(s.rpe)::integer AS rated_sessions,
    COALESCE(SUM(s.rpe), 0)::bigint AS rpe_sum,
    COUNT(*) FILTER (WHERE s.rpe_failed)::integer AS failed_sessions
  FROM sessions s
  WHERE s.user_id = sqlc.arg('user_id')
    AND s.date >= sqlc.arg('window_start')
    AND s.date < sqlc.arg('window_end')
  GROUP BY 1
)
SELECT
  g.week_start,
  COALESCE(w.session_count, 0)::integer AS session_count,
  COALESCE(w.total_seconds, 0)::bigint AS total_seconds,
  COALESCE(w.climbing_seconds, 0)::bigint AS climbing_seconds,
  COALESCE(w.strength_seconds, 0)::bigint AS strength_seconds,
  COALESCE(w.rated_sessions, 0)::integer AS rated_sessions,
  COALESCE(w.rpe_sum, 0)::bigint AS rpe_sum,
  COALESCE(w.failed_sessions, 0)::integer AS failed_sessions
FROM grid g
LEFT JOIN weekly w ON w.week_start = g.week_start
ORDER BY g.week_start;

-- name: GetCoacheeFirstSessionInstant :one
-- When the athlete's recorded history starts, so the chronic load mean can
-- average only weeks that are really part of it. Without this, an athlete who
-- joined three weeks ago would have the silence before they signed up averaged
-- in as rest, and their first weeks would read as a far bigger jump than they
-- were. Null when they have recorded nothing at all.
SELECT MIN(date)::timestamptz AS first_session_at
FROM sessions
WHERE user_id = sqlc.arg('user_id');

-- name: GetCoacheeProgramWeekNumbers :many
-- Which program week each calendar week of the view is, so the chart can be
-- read against the program the coach wrote rather than against bare dates.
-- The week number arithmetic is GetCoachEmptyProgramWeeks', kept identical on
-- purpose so the two never disagree about which week a Monday is.
--
-- Programs may overlap. DISTINCT ON takes the one that started most recently,
-- which is the block the athlete is in now, and falls back on the id so the
-- answer does not depend on the plan order.
WITH grid AS (
  SELECT generate_series(
    sqlc.arg('first_week_start')::date,
    sqlc.arg('last_week_start')::date,
    interval '7 days'
  )::date AS week_start
)
SELECT DISTINCT ON (g.week_start)
  g.week_start,
  p.name AS program_name,
  (((g.week_start - p.start_date) / 7) + 1)::integer AS week_number
FROM grid g
JOIN coach_programs p
  ON p.user_id = sqlc.arg('user_id')
  AND p.coach_id = sqlc.arg('coach_id')
  AND p.start_date <= g.week_start
  AND (p.duration_weeks IS NULL OR g.week_start < p.start_date + (p.duration_weeks * 7))
ORDER BY g.week_start, p.start_date DESC, p.id;
