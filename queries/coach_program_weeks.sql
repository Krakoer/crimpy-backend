-- name: UpsertCoachProgramWeek :one
INSERT INTO coach_program_weeks (program_id, week_number, notes)
VALUES (@program_id, @week_number, @notes)
ON CONFLICT (program_id, week_number) DO UPDATE
  SET notes = EXCLUDED.notes, updated_at = now()
RETURNING *;

-- name: GetCoachProgramWeek :one
SELECT * FROM coach_program_weeks
WHERE program_id = @program_id AND week_number = @week_number;

-- name: GetCoachProgramWeeks :many
SELECT * FROM coach_program_weeks
WHERE program_id = @program_id
ORDER BY week_number;

-- name: DeleteCoachProgramWeek :exec
DELETE FROM coach_program_weeks
WHERE program_id = @program_id AND week_number = @week_number;

-- name: CreateCoachProgramWeekSession :one
INSERT INTO coach_program_week_sessions
  (week_id, training_id, day_of_week, times_per_week, is_everyday, position, notes)
VALUES (@week_id, @training_id, @day_of_week, @times_per_week, @is_everyday, @position, @notes)
RETURNING *;

-- name: UpdateCoachProgramWeekSession :one
UPDATE coach_program_week_sessions
SET training_id    = @training_id,
    day_of_week    = @day_of_week,
    times_per_week = @times_per_week,
    is_everyday    = @is_everyday,
    position       = @position,
    notes          = @notes,
    updated_at     = now()
WHERE id = @id AND week_id = @week_id
RETURNING *;

-- name: GetCoachProgramWeekSessions :many
SELECT
  s.id,
  s.week_id,
  s.training_id,
  s.day_of_week,
  s.times_per_week,
  s.is_everyday,
  s.position,
  s.notes        AS session_notes,
  s.created_at,
  s.updated_at,
  ct.title         AS training_title,
  ct.training_type AS training_type,
  -- Locked once played: the prescription must keep describing what was played.
  -- Only a played session counts. A logged one may hold the link, to complete a
  -- slot with nothing to step through, but must not lock: it is typed in after
  -- the fact, so counting it would hand an athlete a way to freeze their coach's
  -- week by hand.
  EXISTS (
    SELECT 1 FROM sessions played
    WHERE played.program_session_id = s.id AND played.origin = 'played'
  ) AS is_locked
FROM coach_program_week_sessions s
JOIN trainings ct ON ct.id = s.training_id
WHERE s.week_id = @week_id
ORDER BY s.position;

-- Takes the row locks the freeze check reads under, so a session played between
-- the check and the delete cannot slip past it. FOR UPDATE is the only mode that
-- conflicts with the FOR KEY SHARE the sessions FK takes.
-- name: LockCoachProgramWeekSessions :many
SELECT id FROM coach_program_week_sessions
WHERE week_id = @week_id
FOR UPDATE;

-- name: CountPlayedSessionsInWeek :one
SELECT COUNT(*) FROM sessions played
JOIN coach_program_week_sessions s ON s.id = played.program_session_id
JOIN coach_program_weeks w ON w.id = s.week_id
WHERE w.program_id = @program_id AND w.week_number = @week_number
  AND played.origin = 'played';

-- name: CountPlayedSessionsInProgram :one
SELECT COUNT(*) FROM sessions played
JOIN coach_program_week_sessions s ON s.id = played.program_session_id
JOIN coach_program_weeks w ON w.id = s.week_id
WHERE w.program_id = @program_id AND played.origin = 'played';

-- name: DeleteCoachProgramWeekSessionsNotIn :exec
DELETE FROM coach_program_week_sessions
WHERE week_id = @week_id AND NOT (id = ANY(@kept_ids::uuid[]));

-- name: UpsertCoachProgramSessionOverride :one
INSERT INTO coach_program_session_overrides (session_id, item_id, overrides)
VALUES (@session_id, @item_id, @overrides)
ON CONFLICT (session_id, item_id) DO UPDATE
  SET overrides = EXCLUDED.overrides, updated_at = now()
RETURNING *;

-- name: DeleteCoachProgramSessionOverridesNotIn :exec
DELETE FROM coach_program_session_overrides
WHERE session_id = @session_id AND NOT (item_id = ANY(@kept_item_ids::uuid[]));

-- name: GetCoachProgramWeekOverrides :many
SELECT o.*
FROM coach_program_session_overrides o
JOIN coach_program_week_sessions s ON s.id = o.session_id
WHERE s.week_id = @week_id;

-- name: GetCoachProgramWeekSession :one
SELECT * FROM coach_program_week_sessions WHERE id = @id;

-- name: GetCoachProgramSessionOverrides :many
SELECT * FROM coach_program_session_overrides
WHERE session_id = @session_id;
