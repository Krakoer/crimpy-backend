-- The coach feed, derived on read rather than appended to by every write path,
-- so it needs no backfill and cannot silently miss an event a handler forgot to
-- record. One row per thing that happened to one of the coach's coachees. The
-- optional before cursor walks the list backwards: pass the occurred_at of the
-- oldest row already held to get the page under it. The branch carrying a NULL
-- in every optional column comes first so the union reads as nullable there.
-- A declared week is written whole, so its seven rows are one event, dated by
-- the last of them to be touched.
-- name: GetCoachFeed :many
SELECT
  'coachee_enrolled'::text AS kind,
  e.enrolled_at            AS occurred_at,
  e.user_id                AS user_id,
  u.firstname              AS user_firstname,
  u.lastname               AS user_lastname,
  NULL::uuid               AS session_id,
  NULL::text               AS title,
  NULL::integer            AS activity,
  NULL::text               AS origin,
  NULL::text               AS note,
  NULL::date               AS week_start
FROM coach_enrollments e
JOIN users u ON u.id = e.user_id
WHERE e.coach_id = @coach_id
  AND e.enrolled_at < COALESCE(sqlc.narg('before')::timestamptz, 'infinity')

UNION ALL

SELECT
  'session_completed',
  s.date,
  s.user_id,
  u.firstname,
  u.lastname,
  s.id,
  s.name,
  s.activity,
  s.origin,
  NULLIF(s.notes, ''),
  NULL::date
FROM sessions s
JOIN coach_enrollments e ON e.user_id = s.user_id
JOIN users u ON u.id = s.user_id
WHERE e.coach_id = @coach_id
  AND s.date < COALESCE(sqlc.narg('before')::timestamptz, 'infinity')

UNION ALL

SELECT
  'availability_declared',
  MAX(a.updated_at),
  a.user_id,
  u.firstname,
  u.lastname,
  NULL::uuid,
  NULL::text,
  NULL::integer,
  NULL::text,
  NULL::text,
  a.week_start
FROM coachee_day_availabilities a
JOIN coach_enrollments e ON e.user_id = a.user_id
JOIN users u ON u.id = a.user_id
WHERE e.coach_id = @coach_id
GROUP BY a.user_id, a.week_start, u.firstname, u.lastname
HAVING MAX(a.updated_at) < COALESCE(sqlc.narg('before')::timestamptz, 'infinity')

ORDER BY occurred_at DESC
LIMIT @row_limit;

-- The sessions whose notes the coach has not answered. The athlete wrote
-- something and is waiting, which is the first thing the TODO owes them.
-- name: GetCoachPendingSessionFeedback :many
SELECT
  s.id         AS session_id,
  s.user_id    AS user_id,
  u.firstname  AS user_firstname,
  u.lastname   AS user_lastname,
  s.name       AS session_name,
  s.date       AS session_date,
  s.activity   AS activity,
  s.notes      AS notes
FROM sessions s
JOIN coach_enrollments e ON e.user_id = s.user_id
JOIN users u ON u.id = s.user_id
WHERE e.coach_id = @coach_id
  AND s.notes <> ''
  AND s.coach_reply IS NULL
ORDER BY s.date DESC
LIMIT @row_limit;

-- The coach's programs that cover the calendar week starting @week_start and
-- have no session prescribed in it. The program week that week falls in is the
-- whole weeks elapsed since the program started, plus one, so a program whose
-- start date is not a Monday still reports the week the coach would open.
-- name: GetCoachEmptyProgramWeeks :many
WITH target AS (
  SELECT @week_start::date AS week_start
)
SELECT
  p.id        AS program_id,
  p.name      AS program_name,
  p.user_id   AS user_id,
  u.firstname AS user_firstname,
  u.lastname  AS user_lastname,
  (((t.week_start - p.start_date) / 7) + 1)::integer AS week_number
FROM coach_programs p
JOIN users u ON u.id = p.user_id
CROSS JOIN target t
WHERE p.coach_id = @coach_id
  AND p.start_date <= t.week_start
  AND (p.duration_weeks IS NULL
       OR t.week_start < p.start_date + (p.duration_weeks * 7))
  AND NOT EXISTS (
    SELECT 1
    FROM coach_program_weeks w
    JOIN coach_program_week_sessions cs ON cs.week_id = w.id
    WHERE w.program_id = p.id
      AND w.week_number = ((t.week_start - p.start_date) / 7) + 1
  )
ORDER BY u.lastname, u.firstname, p.name;

-- How many sessions the coach's athletes did in one window. The dashboard shows
-- it for the current week, whose bounds only the caller's timezone offset can
-- place, which is why the window arrives as two instants rather than a week.
-- name: CountCoachSessionsInWindow :one
SELECT COUNT(*) FROM sessions s
JOIN coach_enrollments e ON e.user_id = s.user_id
WHERE e.coach_id = @coach_id
  AND s.date >= @window_start
  AND s.date < @window_end;
