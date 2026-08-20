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

-- name: GetUserLatestAssessmentValues :many
-- The athlete's assessment results as they stand: the last value measured for
-- each assessment and each hand, by the date of the session that recorded it.
-- The hands are tracked apart, so a later assessment carrying only one of them
-- does not discard the other hand's last measurement, which is how the clients
-- resolve a percentage of an assessment.
WITH measured AS (
  SELECT a.type, a.right_value, a.left_value, s.date
  FROM assessments a
  JOIN sessions s ON s.id = a.session_id
  WHERE a.user_id = @user_id
),
last_right AS (
  SELECT DISTINCT ON (type) type, right_value
  FROM measured WHERE right_value IS NOT NULL
  ORDER BY type, date DESC
),
last_left AS (
  SELECT DISTINCT ON (type) type, left_value
  FROM measured WHERE left_value IS NOT NULL
  ORDER BY type, date DESC
)
SELECT
  COALESCE(last_right.type, last_left.type)::int AS type,
  last_right.right_value,
  last_left.left_value
FROM last_right
FULL OUTER JOIN last_left ON last_left.type = last_right.type
ORDER BY 1;
