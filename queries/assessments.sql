-- name: CreateAssessment :one
INSERT INTO assessments (user_id, assessment_id, right_value, left_value, session_id, grip_position)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAssessment :one
SELECT * FROM assessments WHERE id = $1;

-- name: GetSessionAssessments :many
-- The results measured in one session, each with the assessment that defines it
-- and the weigh-in it has to be divided by to read as a ratio to bodyweight, so
-- a caller can name, format and divide the number without a second query.
--
-- The denominator is the one bodyweight_for_result answers with, which is the
-- same one the listing and the two date snapshot divide by: a weighted hang the
-- coach opens from the session list has to read as the same ratio it reads as on
-- the assessments tab two clicks away.
--
-- Nothing caps the search here, so it passes 'infinity': a session read is not a
-- read "as of" an instant, and the weigh-in a result was pulled at is decided by
-- the result, not by when the row is fetched.
--
-- The session is joined in for the athlete and the day, which the result row does
-- not carry: the assessment belongs to a session, and the session says whose it is
-- and when it happened.
--
-- bodyweight_kg comes back as zero when no weigh-in qualifies, standing for
-- "unknown" rather than for a weight: user_bodyweights_weight_check keeps a real
-- measurement strictly above zero, so the two cannot be confused. The handler
-- turns it into an absent field before it reaches a client.
--
-- Both columns carry an explicit cast because sqlc reads the schema statically and
-- cannot see through a set returning function to the columns it returns.
SELECT
  a.*,
  d.label,
  d.unit,
  d.per_hand,
  d.bodyweight_relative,
  d.training_id,
  COALESCE(w.weight_kg, 0)::real AS bodyweight_kg,
  w.measured_at::timestamptz AS bodyweight_measured_at
FROM assessments a
JOIN sessions s ON s.id = a.session_id
JOIN assessment_definitions d ON d.id = a.assessment_id
LEFT JOIN LATERAL bodyweight_for_result(s.user_id, s.date, 'infinity'::timestamptz) w ON true
WHERE a.session_id = $1
-- Ordered so a session holding several results draws them the same way twice.
-- Without it the order is whatever the plan yields, and the join and the lateral
-- above are exactly the kind of change that moves a plan. By label because that
-- is what a reader scans the rows by, then by grip, then by the row id so two
-- results on one grip still resolve the same way on every request.
ORDER BY d.label, a.grip_position NULLS FIRST, a.id;

-- name: GetUserAssessments :many
-- Every result the athlete has recorded, each with the assessment that defines
-- it and the weigh-in it has to be divided by to read as a ratio to bodyweight.
--
-- The weigh-in is the one bodyweight_for_result answers with, which is the same
-- rule the two date snapshot and the session detail divide by, because it is the
-- same function. Two rules would let the cards and the comparison panel print two
-- different ratios for one result.
--
-- The snapshot caps its search at the instant it is read as of, which this query
-- has nothing to cap by, so it passes 'infinity'. The two therefore agree for
-- every snapshot asked for a day, which is what a client asks for and what the
-- comparison panel sends, and can differ for one asked for an instant mid day: a
-- weigh-in taken that evening is the denominator here and does not exist yet
-- there.
--
-- It runs for every row rather than only for a bodyweight_relative definition,
-- which costs two index probes on a result nothing will divide. That is the
-- price of one row shape: the flag is free to toggle at any time, since nothing
-- derived from it is stored, and a listing that carried the weight only where
-- the flag is set today would come back without it for a whole history the
-- moment a coach turns the flag on.
--
-- The day is cut in UTC explicitly rather than through a server setting, so the
-- boundary does not move with one.
--
-- Its own date travels with it, because how stale a denominator is decides
-- whether the ratio means anything, and a weight alone cannot say.
--
-- weight_kg comes back as zero when no weigh-in qualifies, standing for
-- "unknown" rather than for a weight: user_bodyweights_weight_check keeps a real
-- measurement strictly above zero, so the two cannot be confused. The handler
-- turns it into an absent field before it reaches a client.
SELECT
  a.*,
  s.date AS session_date,
  d.label,
  d.unit,
  d.per_hand,
  d.bodyweight_relative,
  d.training_id,
  COALESCE(w.weight_kg, 0)::real AS bodyweight_kg,
  w.measured_at::timestamptz AS bodyweight_measured_at
FROM assessments a
JOIN sessions s ON a.session_id = s.id
JOIN assessment_definitions d ON d.id = a.assessment_id
LEFT JOIN LATERAL bodyweight_for_result(s.user_id, s.date, 'infinity'::timestamptz) w ON true
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
-- The weigh-in each value is divided by is the one bodyweight_for_result answers
-- with, the same rule GetUserAssessments and GetSessionAssessments divide by,
-- because it is the same function: the last weigh-in taken at or before the value
-- itself, widening to the rest of that day when nothing precedes it. The widening
-- matters most here, where the snapshot's own bodyweight_kg is bounded by the end
-- of the date asked for and would otherwise name a weight the same response
-- denies to the result pulled that day.
--
-- Both hands are capped by @as_of, so a read "as of" an instant never divides by
-- a weight that did not exist yet at that instant, the same bound
-- GetUserBodyweightAtDate puts on the snapshot's own bodyweight_kg. The other two
-- read paths have nothing to cap by and pass 'infinity'.
--
-- One lookup per hand, since the two hands can come from sessions months apart and
-- each divides by the weight of its own day.
--
-- The date of that weigh-in travels with it, because how stale a denominator is
-- decides whether the ratio means anything, and a weight alone cannot say.
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
  COALESCE(rw.weight_kg, 0)::real AS right_bodyweight_kg,
  rw.measured_at::timestamptz AS right_bodyweight_measured_at,
  l.left_value,
  l.date AS left_measured_at,
  COALESCE(lw.weight_kg, 0)::real AS left_bodyweight_kg,
  lw.measured_at::timestamptz AS left_bodyweight_measured_at
FROM last_right r
FULL OUTER JOIN last_left l
  ON l.assessment_id = r.assessment_id
 AND l.grip_position = r.grip_position
JOIN assessment_definitions d
  ON d.id = COALESCE(r.assessment_id, l.assessment_id)
LEFT JOIN LATERAL bodyweight_for_result(@user_id::uuid, r.date, @as_of::timestamptz) rw ON true
LEFT JOIN LATERAL bodyweight_for_result(@user_id::uuid, l.date, @as_of::timestamptz) lw ON true
ORDER BY d.label, 7, d.id;

-- name: GetResultBodyweight :one
-- The weigh-in one result is divided by, for the recording path, which builds its
-- response from the row it just wrote rather than from a read query and would
-- otherwise answer "no weight on file" for an athlete who has one.
--
-- The same bodyweight_for_result the three read paths divide by, so a result the
-- app has just recorded reads as the ratio the coachee page will show for it.
-- Nothing caps the search, for the same reason the session read caps nothing.
--
-- The dummy row on the left keeps this a one row answer: the function returns no
-- row when no weigh-in qualifies, and a caller asking for the denominator of one
-- result wants "none" rather than an error.
SELECT
  COALESCE(w.weight_kg, 0)::real AS bodyweight_kg,
  w.measured_at::timestamptz AS bodyweight_measured_at
FROM (SELECT 1) AS one
LEFT JOIN LATERAL bodyweight_for_result(@user_id::uuid, @measured_at::timestamptz, 'infinity'::timestamptz) w ON true;
