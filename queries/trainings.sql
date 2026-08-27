-- name: CreateTraining :one
INSERT INTO trainings (user_id, title, description, training_type, goal, comment, is_favorite)
VALUES (@user_id, @title, @description, @training_type, @goal, @comment, @is_favorite)
RETURNING *;

-- name: GetTraining :one
SELECT * FROM trainings WHERE id = @id;

-- name: GetTrainingOwnedBy :one
SELECT id FROM trainings WHERE id = @id AND user_id = @user_id;

-- name: GetTrainings :many
-- @is_assessment is null for the whole library, true for the assessments alone
-- and false for the trainings that are not one.
SELECT t.*, d.id AS assessment_id, d.label, d.prompt, d.unit, d.per_hand
FROM trainings t
LEFT JOIN assessment_definitions d ON d.training_id = t.id
WHERE t.user_id = @user_id
  AND (sqlc.narg('is_assessment')::bool IS NULL
       OR (d.id IS NOT NULL) = sqlc.narg('is_assessment')::bool)
ORDER BY t.title;

-- name: UpdateTraining :one
UPDATE trainings
SET title = @title, description = @description, training_type = @training_type,
    goal = @goal, comment = @comment, is_favorite = @is_favorite, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteTraining :exec
DELETE FROM trainings WHERE id = @id;

-- name: CreateTrainingItem :one
INSERT INTO training_items (
  training_id, parent_id, type, position,
  cycles, cycle_rest_seconds, interval_seconds,
  reps, reps_is_max, duration, rest_seconds,
  exercise_id,
  worktime_seconds, hand, granularity,
  free_text, comment, load_is_max,
  loads, left_loads, hand_positions, edge_sizes_mm,
  variable_targets,
  group_title
) VALUES (
  @training_id, @parent_id, @type, @position,
  @cycles, @cycle_rest_seconds, @interval_seconds,
  @reps, @reps_is_max, @duration, @rest_seconds,
  @exercise_id,
  @worktime_seconds, @hand, @granularity,
  @free_text, @comment, @load_is_max,
  @loads, @left_loads, @hand_positions, @edge_sizes_mm,
  @variable_targets,
  @group_title
)
RETURNING *;

-- name: GetTrainingItems :many
SELECT training_items.*, exercises.name AS exercise_name
FROM training_items
LEFT JOIN exercises ON exercises.id = training_items.exercise_id
WHERE training_items.training_id = @training_id
ORDER BY training_items.position;

-- name: GetTrainingItemInTraining :one
SELECT * FROM training_items WHERE id = @id AND training_id = @training_id;

-- Scoped to the training so an id belonging to another one, and therefore
-- possibly to another coach, updates nothing and is refused by the caller.
-- name: UpdateTrainingItem :one
UPDATE training_items
SET parent_id = @parent_id, type = @type, position = @position,
    cycles = @cycles, cycle_rest_seconds = @cycle_rest_seconds,
    interval_seconds = @interval_seconds,
    reps = @reps, reps_is_max = @reps_is_max, duration = @duration,
    rest_seconds = @rest_seconds,
    exercise_id = @exercise_id,
    worktime_seconds = @worktime_seconds, hand = @hand, granularity = @granularity,
    free_text = @free_text, comment = @comment, load_is_max = @load_is_max,
    loads = @loads, left_loads = @left_loads, hand_positions = @hand_positions,
    edge_sizes_mm = @edge_sizes_mm,
    variable_targets = @variable_targets,
    group_title = @group_title,
    updated_at = now()
WHERE id = @id AND training_id = @training_id
RETURNING *;

-- name: DeleteTrainingItemsNotIn :exec
DELETE FROM training_items
WHERE training_id = @training_id AND NOT (id = ANY(@kept_ids::uuid[]));
