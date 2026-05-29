-- name: CreateRepData :one
INSERT INTO rep_datas (
  user_id, average_weight, session_id, is_rest, right_hand, duration, target_weight, index, grip_position
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetRepData :one
SELECT * FROM rep_datas WHERE id = $1;

-- name: GetSessionRepDatas :many
SELECT * FROM rep_datas WHERE session_id = $1 ORDER BY index;

-- name: DeleteRepData :exec
DELETE FROM rep_datas WHERE id = $1;

-- name: DeleteSessionRepDatas :exec
DELETE FROM rep_datas WHERE session_id = $1;
