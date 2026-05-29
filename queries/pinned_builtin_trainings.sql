-- name: PinBuiltinTraining :one
INSERT INTO pinned_builtin_trainings (builtin_training_id, user_id)
VALUES ($1, $2)
ON CONFLICT (builtin_training_id, user_id) DO UPDATE SET updated_at = now()
RETURNING *;

-- name: GetUserPinnedBuiltinTrainings :many
SELECT * FROM pinned_builtin_trainings WHERE user_id = $1 ORDER BY updated_at DESC;

-- name: UnpinBuiltinTraining :exec
DELETE FROM pinned_builtin_trainings WHERE builtin_training_id = $1 AND user_id = $2;
