-- name: CreateRepTemplate :one
INSERT INTO rep_templates (
  is_rest, right_hand, duration, training_id, target_weight, index, grip_position
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetRepTemplate :one
SELECT * FROM rep_templates WHERE id = $1;

-- name: GetTrainingRepTemplates :many
SELECT * FROM rep_templates WHERE training_id = $1 ORDER BY index;

-- name: DeleteRepTemplate :exec
DELETE FROM rep_templates WHERE id = $1;

-- name: DeleteTrainingRepTemplates :exec
DELETE FROM rep_templates WHERE training_id = $1;
