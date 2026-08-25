-- name: CreateAssessmentDefinition :one
INSERT INTO assessment_definitions (user_id, training_id, label, prompt, unit, per_hand)
VALUES (@user_id, @training_id, @label, @prompt, @unit, @per_hand)
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
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteAssessmentDefinition :exec
DELETE FROM assessment_definitions WHERE id = @id;

-- name: CountRecordableAssessment :one
-- An assessment a result may be recorded against: one Crimpy ships, the
-- caller's own, or a coach's whose training was prescribed to them by a program.
-- Counting rather than selecting keeps an unknown id and a foreign one
-- indistinguishable to the caller.
SELECT COUNT(*) FROM assessment_definitions d
WHERE d.id = @assessment_id
  AND (
    d.user_id IS NULL
    OR d.user_id = @user_id
    OR EXISTS (
      SELECT 1 FROM coach_program_week_sessions s
      JOIN coach_program_weeks w ON w.id = s.week_id
      JOIN coach_programs p ON p.id = w.program_id
      WHERE s.training_id = d.training_id AND p.user_id = @user_id
    )
  );

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
