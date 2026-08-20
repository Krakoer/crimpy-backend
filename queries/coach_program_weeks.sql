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
  EXISTS (
    SELECT 1 FROM sessions played WHERE played.program_session_id = s.id
  ) AS is_locked
FROM coach_program_week_sessions s
JOIN trainings ct ON ct.id = s.training_id
WHERE s.week_id = @week_id
ORDER BY s.position;

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
