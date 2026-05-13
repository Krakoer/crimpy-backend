-- name: CreateTag :one
INSERT INTO tags (coach_id, name, color)
VALUES (@coach_id, @name, @color)
RETURNING *;

-- name: GetTag :one
SELECT * FROM tags WHERE id = @id;

-- name: GetCoachTags :many
SELECT * FROM tags WHERE (coach_id = @coach_id OR is_builtin = TRUE) ORDER BY is_builtin DESC, name;

-- name: UpdateTag :one
UPDATE tags SET name = @name, color = @color, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteTag :exec
DELETE FROM tags WHERE id = @id;

-- name: AssignTagToExercise :exec
INSERT INTO exercise_tags (exercise_id, tag_id) VALUES (@exercise_id, @tag_id)
ON CONFLICT DO NOTHING;

-- name: UnassignTagFromExercise :exec
DELETE FROM exercise_tags WHERE exercise_id = @exercise_id AND tag_id = @tag_id;

-- name: GetExerciseTags :many
SELECT t.* FROM tags t
JOIN exercise_tags et ON et.tag_id = t.id
WHERE et.exercise_id = @exercise_id
ORDER BY t.name;

-- name: GetCoachExerciseTags :many
SELECT et.exercise_id, t.id, t.name, t.color, t.is_builtin, t.created_at, t.updated_at
FROM tags t
JOIN exercise_tags et ON et.tag_id = t.id
WHERE (t.coach_id = @coach_id OR t.is_builtin = TRUE)
ORDER BY et.exercise_id, t.name;

-- name: GetExerciseTagsByIDs :many
SELECT et.exercise_id, t.id, t.name, t.color, t.is_builtin, t.created_at, t.updated_at
FROM tags t
JOIN exercise_tags et ON et.tag_id = t.id
WHERE et.exercise_id = ANY(@exercise_ids::uuid[])
ORDER BY et.exercise_id, t.name;
