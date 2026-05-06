-- name: CreateCoachTraining :one
INSERT INTO coach_trainings (coach_id, title, description)
VALUES (@coach_id, @title, @description)
RETURNING *;

-- name: GetCoachTraining :one
SELECT * FROM coach_trainings WHERE id = @id;

-- name: GetCoachTrainings :many
SELECT * FROM coach_trainings WHERE coach_id = @coach_id ORDER BY title;

-- name: UpdateCoachTraining :one
UPDATE coach_trainings
SET title = @title, description = @description, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteCoachTraining :exec
DELETE FROM coach_trainings WHERE id = @id;

-- name: CreateCoachTrainingItem :one
INSERT INTO coach_training_items (
  training_id, parent_id, type, position,
  cycles, cycle_rest_seconds,
  reps, duration, rest_seconds,
  exercise_id,
  hb_worktime_seconds, both_hands,
  loads, left_loads, hand_positions, edge_sizes_mm,
  section_title
) VALUES (
  @training_id, @parent_id, @type, @position,
  @cycles, @cycle_rest_seconds,
  @reps, @duration, @rest_seconds,
  @exercise_id,
  @hb_worktime_seconds, @both_hands,
  @loads, @left_loads, @hand_positions, @edge_sizes_mm,
  @section_title
)
RETURNING *;

-- name: GetCoachTrainingItems :many
SELECT * FROM coach_training_items WHERE training_id = @training_id ORDER BY position;

-- name: DeleteCoachTrainingItems :exec
DELETE FROM coach_training_items WHERE training_id = @training_id;
