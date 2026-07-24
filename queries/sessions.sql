-- name: CreateSession :one
INSERT INTO sessions (
  user_id, name, notes, is_assessment, session_type, duration, date,
  repeater_sets, repeater_reps, repeater_work_time, repeater_rest_time,
  repeater_set_rest, repeater_split_hand
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
) RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = $1;

-- name: GetUserSessions :many
SELECT * FROM sessions WHERE user_id = $1 ORDER BY date DESC;

-- name: UpdateSession :one
UPDATE sessions
SET name = $2, notes = $3, duration = $4
WHERE id = $1
RETURNING *;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1;
