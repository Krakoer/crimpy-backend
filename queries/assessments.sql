-- name: CreateAssessment :one
INSERT INTO assessments (user_id, type, right_value, left_value, session_id, grip_position)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAssessment :one
SELECT * FROM assessments WHERE id = $1;

-- name: GetSessionAssessments :many
SELECT * FROM assessments WHERE session_id = $1;

-- name: GetUserAssessments :many
SELECT a.*, s.date AS session_date FROM assessments a
JOIN sessions s ON a.session_id = s.id
WHERE s.user_id = $1
ORDER BY s.date DESC;

-- name: DeleteAssessment :exec
DELETE FROM assessments WHERE id = $1;
