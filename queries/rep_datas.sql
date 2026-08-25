-- name: CreateRepData :one
INSERT INTO rep_datas (
  user_id, average_weight, session_id, is_rest, hand, duration, target_weight, index, grip_position, edge_size_mm, training_item_id, target_unmeasured
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetSessionRepDatas :many
SELECT * FROM rep_datas WHERE session_id = $1 ORDER BY index;
