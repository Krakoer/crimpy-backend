-- name: CreateExercise :one
INSERT INTO exercises (coach_id, name, description, comment, video_link)
VALUES (@coach_id, @name, @description, @comment, @video_link)
RETURNING *;

-- name: GetExercise :one
SELECT * FROM exercises WHERE id = @id;

-- name: GetCoachExercises :many
SELECT * FROM exercises WHERE coach_id = @coach_id ORDER BY name;

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
