-- name: CreateAssessment :one
INSERT INTO assessments (user_id, assessment_id, right_value, left_value, session_id, grip_position)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAssessment :one
SELECT * FROM assessments WHERE id = $1;

-- name: GetSessionAssessments :many
-- The results measured in one session, each with the assessment that defines it,
-- so a caller can name and format the number without a second query.
SELECT a.*, d.label, d.unit, d.per_hand, d.training_id
FROM assessments a
JOIN assessment_definitions d ON d.id = a.assessment_id
WHERE a.session_id = $1;

-- name: GetUserAssessments :many
SELECT a.*, s.date AS session_date, d.label, d.unit, d.per_hand, d.training_id
FROM assessments a
JOIN sessions s ON a.session_id = s.id
JOIN assessment_definitions d ON d.id = a.assessment_id
WHERE s.user_id = $1
ORDER BY s.date DESC;

-- name: DeleteAssessment :exec
DELETE FROM assessments WHERE id = $1;

-- name: CountAssessmentsForDefinition :one
-- How many results were measured against a definition, so an edit that would
-- change what those numbers mean can be refused rather than silently applied.
SELECT COUNT(*) FROM assessments WHERE assessment_id = @assessment_id;

-- name: GetUserLatestAssessmentValues :many
-- The athlete's assessment results as they stand: the last value measured for
-- each assessment and each hand, by the date of the session that recorded it.
-- The hands are tracked apart, so a later assessment carrying only one of them
-- does not discard the other hand's last measurement, which is how the clients
-- resolve a percentage of an assessment.
WITH measured AS (
  SELECT a.assessment_id, a.right_value, a.left_value, s.date
  FROM assessments a
  JOIN sessions s ON s.id = a.session_id
  WHERE a.user_id = @user_id
),
last_right AS (
  SELECT DISTINCT ON (assessment_id) assessment_id, right_value
  FROM measured WHERE right_value IS NOT NULL
  ORDER BY assessment_id, date DESC
),
last_left AS (
  SELECT DISTINCT ON (assessment_id) assessment_id, left_value
  FROM measured WHERE left_value IS NOT NULL
  ORDER BY assessment_id, date DESC
)
SELECT
  COALESCE(last_right.assessment_id, last_left.assessment_id) AS assessment_id,
  last_right.right_value,
  last_left.left_value
FROM last_right
FULL OUTER JOIN last_left ON last_left.assessment_id = last_right.assessment_id
ORDER BY 1;
