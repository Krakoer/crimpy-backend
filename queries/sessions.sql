-- name: CreateSession :one
INSERT INTO sessions (
  user_id, name, notes, is_assessment, activity, origin, training_id,
  program_session_id, duration, date,
  repeater_sets, repeater_reps, repeater_work_time, repeater_rest_time,
  repeater_set_rest, repeater_split_hand
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
) RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = $1;

-- name: GetUserSessions :many
-- Carries the rep count so the history list can say how many reps a session
-- holds without fetching every rep of every session.
SELECT sessions.*, COUNT(rep_datas.id) AS rep_count
FROM sessions
LEFT JOIN rep_datas ON rep_datas.session_id = sessions.id
WHERE sessions.user_id = $1
GROUP BY sessions.id
ORDER BY sessions.date DESC;

-- name: UpdateSession :one
UPDATE sessions
SET name = $2, notes = $3, duration = $4, date = COALESCE(sqlc.narg('date'), date),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1;

-- name: CountAccessibleTraining :one
-- A training a session may claim to have been played from: the caller's own, or
-- one prescribed to them by a program. Counting rather than selecting keeps an
-- unknown id and a foreign one indistinguishable to the caller.
SELECT COUNT(*) FROM trainings
WHERE trainings.id = sqlc.arg('training_id')
  AND (
    trainings.user_id = sqlc.arg('user_id')
    OR EXISTS (
      SELECT 1 FROM coach_program_week_sessions
      JOIN coach_program_weeks ON coach_program_weeks.id = coach_program_week_sessions.week_id
      JOIN coach_programs ON coach_programs.id = coach_program_weeks.program_id
      WHERE coach_program_week_sessions.training_id = trainings.id
        AND coach_programs.user_id = sqlc.arg('user_id')
    )
  );

-- name: CountAccessibleProgramSession :one
-- A prescribed session is the caller's when it sits in a program assigned to them.
SELECT COUNT(*) FROM coach_program_week_sessions
JOIN coach_program_weeks ON coach_program_weeks.id = coach_program_week_sessions.week_id
JOIN coach_programs ON coach_programs.id = coach_program_weeks.program_id
WHERE coach_program_week_sessions.id = sqlc.arg('program_session_id')
  AND coach_programs.user_id = sqlc.arg('user_id');
