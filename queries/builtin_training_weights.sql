-- name: CreateBuiltinTrainingWeight :one
-- Upsert: a user has at most one override per builtin training, so a second save
-- for the same pair updates it instead of failing on the unique constraint. The
-- conflict target includes user_id, so this can only ever touch the caller's row.
INSERT INTO builtin_training_weights (id, user_id, builtin_training_id, custom_weight_left, custom_weight_right)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, builtin_training_id) DO UPDATE
SET custom_weight_left = EXCLUDED.custom_weight_left,
    custom_weight_right = EXCLUDED.custom_weight_right,
    updated_at = now()
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
