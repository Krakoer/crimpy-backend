-- name: CreateCoachSession :one
INSERT INTO coach_sessions (coach_id, title, description)
VALUES (@coach_id, @title, @description)
RETURNING *;

-- name: GetCoachSession :one
SELECT * FROM coach_sessions WHERE id = @id;

-- name: GetCoachSessions :many
SELECT * FROM coach_sessions WHERE coach_id = @coach_id ORDER BY title;

-- name: UpdateCoachSession :one
UPDATE coach_sessions
SET title = @title, description = @description, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteCoachSession :exec
DELETE FROM coach_sessions WHERE id = @id;

-- name: CreateCoachSessionItem :one
INSERT INTO coach_session_items (
  session_id, parent_id, type, position,
  cycles, cycle_rest_seconds,
  reps, reps_unit, rest_seconds,
  exercise_id,
  hb_worktime_seconds, both_hands,
  loads, hand_positions, edge_sizes_mm,
  section_title
) VALUES (
  @session_id, @parent_id, @type, @position,
  @cycles, @cycle_rest_seconds,
  @reps, @reps_unit, @rest_seconds,
  @exercise_id,
  @hb_worktime_seconds, @both_hands,
  @loads, @hand_positions, @edge_sizes_mm,
  @section_title
)
RETURNING *;

-- name: GetCoachSessionItems :many
SELECT * FROM coach_session_items WHERE session_id = @session_id ORDER BY position;

-- name: DeleteCoachSessionItems :exec
DELETE FROM coach_session_items WHERE session_id = @session_id;
