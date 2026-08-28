-- name: CreateSession :one
INSERT INTO sessions (
  user_id, name, notes, is_assessment, activity, origin, training_id,
  program_session_id, prescription, samples, duration, date
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
) RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = $1;

-- name: GetUserSessions :many
-- Carries the rep count so the history list can say how many reps a session
-- holds without fetching every rep of every session. The columns are listed out
-- rather than starred so the prescription snapshot stays off the list: it is a
-- whole training per row, it is only read by the detail screen, and a history of
-- a few hundred played sessions would otherwise ship megabytes to render a list.
SELECT
  sessions.id,
  sessions.user_id,
  sessions.name,
  sessions.notes,
  sessions.date,
  sessions.is_assessment,
  sessions.activity,
  sessions.origin,
  sessions.training_id,
  sessions.program_session_id,
  sessions.duration,
  sessions.coach_reply,
  sessions.coach_reply_at,
  sessions.coach_reply_read_at,
  sessions.updated_at,
  COUNT(rep_datas.id) AS rep_count
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

-- name: SetSessionCoachReply :one
-- Writes the coach's answer to a session and marks it unread again: an answer
-- the coach rewrote is not one the athlete has seen.
--
-- Scoped by the athlete the caller verified is their client, so the ownership
-- check and the write are one statement: reading the row first left a window in
-- which the athlete could delete it, and turned a normal race into a 500.
UPDATE sessions
SET coach_reply = sqlc.arg('coach_reply'),
    coach_reply_at = now(),
    coach_reply_read_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id') AND user_id = sqlc.arg('user_id')
RETURNING *;

-- name: ClearSessionCoachReply :one
-- Takes the answer back, leaving the session as if it had never been answered.
-- Scoped the way SetSessionCoachReply is, for the same reason.
UPDATE sessions
SET coach_reply = NULL,
    coach_reply_at = NULL,
    coach_reply_read_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id') AND user_id = sqlc.arg('user_id')
RETURNING *;

-- name: MarkSessionCoachReplyRead :one
-- Stamps the answer as seen. The first read wins, so reopening the session does
-- not keep moving the instant the athlete actually read it.
UPDATE sessions
SET coach_reply_read_at = COALESCE(coach_reply_read_at, now())
WHERE id = $1 AND coach_reply IS NOT NULL
RETURNING *;
