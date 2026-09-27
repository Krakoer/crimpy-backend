-- name: CreateUserBodyweight :one
INSERT INTO user_bodyweights (user_id, weight_kg, measured_at)
VALUES (@user_id, @weight_kg, @measured_at)
RETURNING *;

-- name: GetUserBodyweights :many
SELECT * FROM user_bodyweights
WHERE user_id = @user_id
ORDER BY measured_at DESC, created_at DESC
LIMIT @row_limit;

-- name: GetUserLatestBodyweight :one
SELECT * FROM user_bodyweights
WHERE user_id = @user_id
ORDER BY measured_at DESC, created_at DESC
LIMIT 1;

-- name: GetUserBodyweightAtDate :one
-- The weight in effect on a given day, which is the denominator a bodyweight
-- relative score measured then has to be read against. The last measurement at
-- or before the date rather than the nearest one: a weigh-in that happened after
-- the test cannot be what the test was pulled at.
SELECT * FROM user_bodyweights
WHERE user_id = @user_id AND measured_at <= @as_of
ORDER BY measured_at DESC, created_at DESC
LIMIT 1;

-- name: GetUserBodyweight :one
SELECT * FROM user_bodyweights WHERE id = @id;

-- name: DeleteUserBodyweight :exec
DELETE FROM user_bodyweights WHERE id = @id;
