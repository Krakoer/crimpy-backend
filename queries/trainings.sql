-- name: CreateTraining :one
INSERT INTO trainings (user_id, title, description, training_type, goal, comment, is_favorite)
VALUES (@user_id, @title, @description, @training_type, @goal, @comment, @is_favorite)
RETURNING *;

-- name: GetTraining :one
SELECT * FROM trainings WHERE id = @id;

-- name: GetTrainings :many
SELECT * FROM trainings WHERE user_id = @user_id ORDER BY title;

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
  cycles, cycle_rest_seconds,
  reps, duration, rest_seconds,
  exercise_id,
  worktime_seconds, hand,
  free_text, comment, load_is_max,
  loads, left_loads, hand_positions, edge_sizes_mm,
  section_title
) VALUES (
  @training_id, @parent_id, @type, @position,
  @cycles, @cycle_rest_seconds,
  @reps, @duration, @rest_seconds,
  @exercise_id,
  @worktime_seconds, @hand,
  @free_text, @comment, @load_is_max,
  @loads, @left_loads, @hand_positions, @edge_sizes_mm,
  @section_title
)
RETURNING *;

-- name: GetTrainingItems :many
SELECT training_items.*, exercises.name AS exercise_name
FROM training_items
LEFT JOIN exercises ON exercises.id = training_items.exercise_id
WHERE training_items.training_id = @training_id
ORDER BY training_items.position;

-- name: DeleteTrainingItems :exec
DELETE FROM training_items WHERE training_id = @training_id;
