-- name: CreateCoachTraining :one
INSERT INTO coach_trainings (user_id, title, description, training_type, goal, comment, is_favorite)
VALUES (@user_id, @title, @description, @training_type, @goal, @comment, @is_favorite)
RETURNING *;

-- name: GetCoachTraining :one
SELECT * FROM coach_trainings WHERE id = @id;

-- name: GetCoachTrainings :many
SELECT * FROM coach_trainings WHERE user_id = @user_id ORDER BY title;

-- name: UpdateCoachTraining :one
UPDATE coach_trainings
SET title = @title, description = @description, training_type = @training_type,
    goal = @goal, comment = @comment, is_favorite = @is_favorite, updated_at = now()
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
  worktime_seconds, hand,
  free_text, load_is_max,
  loads, left_loads, hand_positions, edge_sizes_mm,
  section_title
) VALUES (
  @training_id, @parent_id, @type, @position,
  @cycles, @cycle_rest_seconds,
  @reps, @duration, @rest_seconds,
  @exercise_id,
  @worktime_seconds, @hand,
  @free_text, @load_is_max,
  @loads, @left_loads, @hand_positions, @edge_sizes_mm,
  @section_title
)
RETURNING *;

-- name: GetCoachTrainingItems :many
SELECT * FROM coach_training_items WHERE training_id = @training_id ORDER BY position;

-- name: DeleteCoachTrainingItems :exec
DELETE FROM coach_training_items WHERE training_id = @training_id;
