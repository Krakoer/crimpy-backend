-- name: CreateAssessment :one
INSERT INTO assessments (user_id, assessment_id, right_value, left_value, session_id, grip_position)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAssessment :one
SELECT * FROM assessments WHERE id = $1;

-- name: GetSessionAssessments :many
-- The results measured in one session, each with the assessment that defines it,
-- so a caller can name and format the number without a second query.
SELECT a.*, d.label, d.unit, d.per_hand, d.bodyweight_relative, d.training_id
FROM assessments a
JOIN assessment_definitions d ON d.id = a.assessment_id
WHERE a.session_id = $1;

-- name: GetUserAssessments :many
SELECT a.*, s.date AS session_date, d.label, d.unit, d.per_hand, d.bodyweight_relative, d.training_id
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

-- name: GetUserAssessmentValuesAtDate :many
-- The athlete's assessment results as they stood on a given day: for each
-- assessment, each grip and each hand, the last value measured at or before it.
-- GetUserLatestAssessmentValues is this query with no upper bound, and the hands
-- are tracked apart here for the same reason: an assessment carrying only one of
-- them does not discard the other hand's last measurement.
--
-- The grip is part of the key rather than collapsed away, because a hang on a
-- 20mm edge and one on a 10mm edge are different tests to a coach reading two
-- dates side by side, and the results carry the grip they were pulled on.
--
-- The date each value was measured travels with it, so a reader can tell a value
-- measured near the date asked for from one carried forward from months back.
--
-- So does the bodyweight that value was pulled at, which is not the same thing as
-- the athlete's weight on the date asked for: the snapshot carries a result
-- forward, and dividing a result measured in March by a weight recorded in June
-- gives a ratio the athlete never achieved. One weight per hand, because the two
-- hands can come from different sessions months apart.
--
-- The weigh-in is bounded by the end of the value's own day rather than by the
-- instant it was measured at, because an athlete who weighs themselves after
-- training rather than before still weighed that on the day, and the snapshot's
-- own bodyweight_kg, bounded by the end of the date asked for, would otherwise
-- name a weight the same response denies to the result pulled that day.
--
-- Those two come back as zero when no weigh-in precedes the value, standing for
-- "unknown" rather than for a weight: user_bodyweights_weight_check keeps a real
-- measurement strictly above zero, so the two cannot be confused. The handler
-- turns it into an absent field before it reaches a client, and a caller never
-- sees the sentinel.
--
-- A later result wins a tie on the date, and the row id settles a tie on both, so
-- two results logged to the same instant resolve the same way on every request. A
-- comparison that flips between identical reads is worse than either answer.
--
-- The definition is joined in, as the other read paths do, so a caller can name
-- and format the number without a second query.
WITH measured AS (
  SELECT
    a.id,
    a.assessment_id,
    COALESCE(a.grip_position, 0)::int AS grip_position,
    a.right_value,
    a.left_value,
    a.updated_at,
    s.date
  FROM assessments a
  JOIN sessions s ON s.id = a.session_id
  WHERE a.user_id = @user_id AND s.date <= @as_of
),
last_right AS (
  SELECT DISTINCT ON (assessment_id, grip_position)
    assessment_id, grip_position, right_value, date
  FROM measured WHERE right_value IS NOT NULL
  ORDER BY assessment_id, grip_position, date DESC, updated_at DESC, id DESC
),
last_left AS (
  SELECT DISTINCT ON (assessment_id, grip_position)
    assessment_id, grip_position, left_value, date
  FROM measured WHERE left_value IS NOT NULL
  ORDER BY assessment_id, grip_position, date DESC, updated_at DESC, id DESC
)
SELECT
  d.id AS assessment_id,
  d.label,
  d.unit,
  d.per_hand,
  d.bodyweight_relative,
  d.training_id,
  COALESCE(r.grip_position, l.grip_position)::int AS grip_position,
  r.right_value,
  r.date AS right_measured_at,
  COALESCE((
    SELECT w.weight_kg FROM user_bodyweights w
    WHERE w.user_id = @user_id
      AND w.measured_at < date_trunc('day', r.date) + interval '1 day' 
    ORDER BY w.measured_at DESC, w.created_at DESC
    LIMIT 1
  ), 0)::real AS right_bodyweight_kg,
  l.left_value,
  l.date AS left_measured_at,
  COALESCE((
    SELECT w.weight_kg FROM user_bodyweights w
    WHERE w.user_id = @user_id
      AND w.measured_at < date_trunc('day', l.date) + interval '1 day' 
    ORDER BY w.measured_at DESC, w.created_at DESC
    LIMIT 1
  ), 0)::real AS left_bodyweight_kg
FROM last_right r
FULL OUTER JOIN last_left l
  ON l.assessment_id = r.assessment_id
 AND l.grip_position = r.grip_position
JOIN assessment_definitions d
  ON d.id = COALESCE(r.assessment_id, l.assessment_id)
ORDER BY d.label, 7, d.id;
