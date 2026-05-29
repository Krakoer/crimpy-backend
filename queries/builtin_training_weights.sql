-- name: CreateBuiltinTrainingWeight :one
INSERT INTO builtin_training_weights (id, user_id, builtin_traning_id, custom_weight_left, custom_weight_right)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserBuiltinTrainingWeights :many
SELECT * FROM builtin_training_weights WHERE user_id = $1;

-- name: GetBuiltinTrainingWeight :one
SELECT * FROM builtin_training_weights WHERE id = $1;

-- name: UpdateBuiltinTrainingWeight :one
UPDATE builtin_training_weights
SET custom_weight_left = $2, custom_weight_right = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteBuiltinTrainingWeight :exec
DELETE FROM builtin_training_weights WHERE id = $1;
