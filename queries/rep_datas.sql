-- name: CreateRepData :one
INSERT INTO rep_datas (
  user_id, average_weight, session_id, is_rest, hand, duration, target_weight, index, grip_position, edge_size_mm, training_item_id, target_unmeasured
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetSessionRepDatas :many
SELECT * FROM rep_datas WHERE session_id = $1 ORDER BY index;

-- name: GetUserRepDatas :many
-- Every rep of every session the user owns, for a client that reads the whole
-- history at once rather than one session at a time. Scoped by the session's
-- owner, not by the rep's own user column, so the rep of a session is exactly
-- what the detail of that session would answer.
SELECT rep_datas.* FROM rep_datas
JOIN sessions ON sessions.id = rep_datas.session_id
WHERE sessions.user_id = $1
ORDER BY rep_datas.session_id, rep_datas.index;
