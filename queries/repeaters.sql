-- name: CreateRepeater :one
INSERT INTO repeaters (
  sets, reps, worktime, resttime, set_rest,
  target_weight_right, target_weight_left, split_hand, grip_position
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetRepeater :one
SELECT * FROM repeaters WHERE id = $1;

-- name: UpdateRepeater :one
UPDATE repeaters
SET sets = $2, reps = $3, worktime = $4, resttime = $5, set_rest = $6,
    target_weight_right = $7, target_weight_left = $8, split_hand = $9, grip_position = $10
WHERE id = $1
RETURNING *;

-- name: DeleteRepeater :exec
DELETE FROM repeaters WHERE id = $1;
