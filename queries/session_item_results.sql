-- name: CreateSessionItemResult :one
INSERT INTO session_item_results (
  session_id, user_id, training_item_id, occurrence,
  reps, cycles, load_kg, duration_seconds, note
) VALUES (
  @session_id, @user_id, @training_item_id, @occurrence,
  @reps, @cycles, @load_kg, @duration_seconds, @note
)
RETURNING *;

-- name: GetSessionItemResults :many
SELECT * FROM session_item_results
WHERE session_id = @session_id
ORDER BY training_item_id, occurrence;
