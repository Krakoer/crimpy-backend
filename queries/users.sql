-- queries/users.sql
-- name: CreateUser :one
INSERT INTO users (email, firstname, lastname, password) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: CreateCoachUser :one
INSERT INTO users (email, firstname, lastname, password, is_coach) VALUES ($1, $2, $3, $4, true) RETURNING *;

-- name: CreateAdminUser :one
INSERT INTO users (email, firstname, lastname, password, is_admin, email_verified) VALUES ($1, $2, $3, $4, true, true) RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users SET password = $2 WHERE id = $1;

-- name: GetPendingCoaches :many
SELECT * FROM users WHERE is_coach = true AND coach_validated = false AND email_verified = true ORDER BY created_at ASC;

-- name: ValidateCoach :exec
UPDATE users SET coach_validated = true WHERE id = $1 AND is_coach = true;

-- name: RejectCoach :exec
UPDATE users SET is_coach = false, coach_validated = false WHERE id = $1;

-- name: SetVerificationToken :exec
UPDATE users SET verification_token = $2, verification_token_expires_at = $3, verification_email_sent_at = NOW() WHERE id = $1;

-- name: GetUserByVerificationToken :one
SELECT * FROM users WHERE verification_token = $1 AND verification_token_expires_at > NOW();

-- name: VerifyUserEmail :exec
UPDATE users SET email_verified = true, verification_token = NULL, verification_token_expires_at = NULL WHERE id = $1;

-- name: GetVerificationEmailSentAt :one
SELECT verification_email_sent_at FROM users WHERE email = $1;
