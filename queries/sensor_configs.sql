-- name: CreateSensorConfig :one
INSERT INTO sensor_configs (id, user_id, name, index, tare, coef)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetUserSensorConfigs :many
SELECT * FROM sensor_configs WHERE user_id = $1 ORDER BY index;

-- name: GetSensorConfig :one
SELECT * FROM sensor_configs WHERE id = $1;

-- name: UpdateSensorConfig :one
UPDATE sensor_configs
SET name = $2, index = $3, tare = $4, coef = $5, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteSensorConfig :exec
DELETE FROM sensor_configs WHERE id = $1;
