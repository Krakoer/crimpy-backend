-- name: CreateTraining :one
INSERT INTO trainings (user_id, name, repeater_id, is_favorite, is_assessment)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetTraining :one
SELECT * FROM trainings WHERE id = $1;

-- name: GetUserTrainings :many
SELECT * FROM trainings WHERE user_id = $1 ORDER BY name;

-- name: GetUserFavoriteTrainings :many
SELECT * FROM trainings WHERE user_id = $1 AND is_favorite = true ORDER BY name;

-- name: UpdateTraining :one
UPDATE trainings
SET name = $2, is_favorite = $3
WHERE id = $1
RETURNING *;

-- name: DeleteTraining :exec
DELETE FROM trainings WHERE id = $1;
