-- name: CreateExercise :one
INSERT INTO exercises (coach_id, name, description, comment, video_link)
VALUES (@coach_id, @name, @description, @comment, @video_link)
RETURNING *;

-- name: GetExercise :one
SELECT * FROM exercises WHERE id = @id;

-- name: UpdateExercise :one
UPDATE exercises
SET name = @name, description = @description, comment = @comment, video_link = @video_link, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteExercise :exec
DELETE FROM exercises WHERE id = @id;

-- name: SetExerciseFavorite :one
UPDATE exercises SET is_favorite = @is_favorite, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GetCoachFavoriteExercises :many
SELECT * FROM exercises WHERE coach_id = @coach_id AND is_favorite = true ORDER BY name;

-- name: SearchCoachExercises :many
SELECT * FROM exercises
WHERE coach_id = @coach_id
  AND name ILIKE '%' || @name_filter::text || '%'
  AND (array_length(@tag_ids::uuid[], 1) IS NULL OR EXISTS (
    SELECT 1 FROM exercise_tags WHERE exercise_id = exercises.id AND tag_id = ANY(@tag_ids::uuid[])
  ))
ORDER BY name
LIMIT @lim OFFSET @off;

-- name: CountCoachExercises :one
SELECT COUNT(*) FROM exercises
WHERE coach_id = @coach_id
  AND name ILIKE '%' || @name_filter::text || '%'
  AND (array_length(@tag_ids::uuid[], 1) IS NULL OR EXISTS (
    SELECT 1 FROM exercise_tags WHERE exercise_id = exercises.id AND tag_id = ANY(@tag_ids::uuid[])
  ));

-- Counts how many of @ids the owner actually holds, so a training that
-- references an exercise can be refused before it stores a reference to
-- somebody else's. A missing id counts the same as an unowned one: both mean
-- the caller cannot name it.
-- name: CountExercisesOwnedBy :one
SELECT COUNT(*) FROM exercises
WHERE id = ANY(@ids::uuid[]) AND coach_id = @coach_id;
