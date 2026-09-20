-- name: CreateAssessmentDefinition :one
INSERT INTO assessment_definitions (user_id, training_id, label, prompt, unit, per_hand, bodyweight_relative)
VALUES (@user_id, @training_id, @label, @prompt, @unit, @per_hand, @bodyweight_relative)
RETURNING *;

-- name: GetAssessmentDefinition :one
SELECT * FROM assessment_definitions WHERE id = @id;

-- name: GetAssessmentDefinitions :many
-- Every assessment the caller may reference: the ones Crimpy ships, which belong
-- to nobody, and their own. Builtins first so a picker lists them in a stable
-- order whatever the athlete has written since.
SELECT * FROM assessment_definitions
WHERE user_id IS NULL OR user_id = @user_id
ORDER BY user_id NULLS FIRST, label;

-- name: GetAssessmentDefinitionsByIDs :many
-- The named definitions, restricted to the ones the caller may reference. A row
-- that does not exist, or belongs to somebody else, is simply absent, and the
-- caller refuses the reference as unknown rather than reporting which it was.
SELECT * FROM assessment_definitions
WHERE id = ANY(@ids::uuid[]) AND (user_id IS NULL OR user_id = @user_id);

-- name: GetAssessmentDefinitionsForPrescription :many
-- The definitions a prescription references, named by ids it already holds. No
-- user scoping: a coachee legitimately reads the coach's definition for a
-- training they were assigned.
SELECT * FROM assessment_definitions WHERE id = ANY(@ids::uuid[]);

-- name: GetAssessmentDefinitionByTraining :one
SELECT * FROM assessment_definitions WHERE training_id = @training_id;

-- name: UpdateAssessmentDefinition :one
UPDATE assessment_definitions
SET label = @label, prompt = @prompt, unit = @unit, per_hand = @per_hand,
    bodyweight_relative = @bodyweight_relative, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteAssessmentDefinition :exec
DELETE FROM assessment_definitions WHERE id = @id;

-- name: GetRecordableAssessmentDefinitions :many
-- The assessments a result may be recorded against: the ones Crimpy ships, the
-- caller's own, and a coach's whose training was prescribed to them by a
-- program. Distinct from GetAssessmentDefinitions, which serves a catalog to
-- pick from and so stops at the builtins and the caller's own.
--
-- program_id names the program the prescription was read under, and is null
-- when nothing prescribes the training. It is what makes such a row usable: a
-- coach's training is only readable under a program of the caller's, so a row
-- that reaches the caller by prescription alone carries the id that reads it.
-- Any of them will do when several programs prescribe the same training, so the
-- oldest is taken and the pick does not move between two calls.
--
-- assessment_id narrows the answer to a single member of that set, which is how
-- the record path asks whether one assessment may be written against. Omitting
-- it asks for the whole set. One query rather than two, so the endpoint that
-- lists them and the check that admits a result cannot come to disagree: an
-- assessment offered by the first and refused by the second is a dead end the
-- athlete only meets once they have already pulled.
--
-- The single id form answers with no row for an unknown assessment and for a
-- foreign one alike, so neither reports which assessments exist.
SELECT sqlc.embed(d), prescribed.program_id
FROM assessment_definitions d
LEFT JOIN LATERAL (
  SELECT p.id AS program_id
  FROM coach_program_week_sessions s
  JOIN coach_program_weeks w ON w.id = s.week_id
  JOIN coach_programs p ON p.id = w.program_id
  WHERE s.training_id = d.training_id AND p.user_id = @user_id
  ORDER BY p.created_at, p.id
  LIMIT 1
) prescribed ON TRUE
WHERE (sqlc.narg('assessment_id')::uuid IS NULL OR d.id = sqlc.narg('assessment_id')::uuid)
  AND (
    d.user_id IS NULL
    OR d.user_id = @user_id
    OR prescribed.program_id IS NOT NULL
  )
ORDER BY d.user_id NULLS FIRST, d.label;

-- name: CountReferencesToAssessment :one
-- How many training items read a number against this assessment, counting both
-- the items themselves and the per-item overrides a coach set on a program week.
-- The reference lives inside opaque JSON, so it is matched by looking for the id
-- rather than by a foreign key.
SELECT (
  SELECT COUNT(*) FROM training_items i
  WHERE i.variable_targets::text LIKE '%' || @assessment_id::text || '%'
     OR i.loads::text LIKE '%' || @assessment_id::text || '%'
     OR i.left_loads::text LIKE '%' || @assessment_id::text || '%'
) + (
  SELECT COUNT(*) FROM coach_program_session_overrides o
  WHERE o.overrides::text LIKE '%' || @assessment_id::text || '%'
) AS total;
