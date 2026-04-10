-- queries/users.sql
-- name: CreateUser :one
INSERT INTO users (email, firstname, lastname, password) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: CreateCoachUser :one
INSERT INTO users (email, firstname, lastname, password, is_coach) VALUES ($1, $2, $3, $4, true) RETURNING *;

-- name: CreateAdminUser :one
INSERT INTO users (email, firstname, lastname, password, is_admin) VALUES ($1, $2, $3, $4, true) RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users SET password = $2 WHERE id = $1;

-- name: GetPendingCoaches :many
SELECT * FROM users WHERE is_coach = true AND coach_validated = false ORDER BY created_at ASC;

-- name: ValidateCoach :exec
UPDATE users SET coach_validated = true WHERE id = $1 AND is_coach = true;

-- name: RejectCoach :exec
UPDATE users SET is_coach = false, coach_validated = false WHERE id = $1;
