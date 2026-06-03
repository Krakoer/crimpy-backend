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
  (week_id, training_id, day_of_week, times_per_week, position, notes)
VALUES (@week_id, @training_id, @day_of_week, @times_per_week, @position, @notes)
RETURNING *;

-- name: GetCoachProgramWeekSessions :many
SELECT
  s.id,
  s.week_id,
  s.training_id,
  s.day_of_week,
  s.times_per_week,
  s.position,
  s.notes        AS session_notes,
  s.created_at,
  s.updated_at,
  ct.title         AS training_title,
  ct.training_type AS training_type
FROM coach_program_week_sessions s
JOIN trainings ct ON ct.id = s.training_id
WHERE s.week_id = @week_id
ORDER BY s.position;

-- name: DeleteCoachProgramWeekSessions :exec
DELETE FROM coach_program_week_sessions WHERE week_id = @week_id;

-- name: UpsertCoachProgramSessionOverride :one
INSERT INTO coach_program_session_overrides (session_id, item_id, overrides)
VALUES (@session_id, @item_id, @overrides)
ON CONFLICT (session_id, item_id) DO UPDATE
  SET overrides = EXCLUDED.overrides, updated_at = now()
RETURNING *;

-- name: GetCoachProgramWeekOverrides :many
SELECT o.*
FROM coach_program_session_overrides o
JOIN coach_program_week_sessions s ON s.id = o.session_id
WHERE s.week_id = @week_id;

-- name: DeleteCoachProgramSessionOverrides :exec
DELETE FROM coach_program_session_overrides WHERE session_id = @session_id;
