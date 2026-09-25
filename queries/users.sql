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

-- name: UpdateUserPassword :execrows
UPDATE users
SET password = $2, password_reset_token_hash = NULL, password_reset_requested_at = NULL
WHERE id = $1;

-- name: SetPasswordResetToken :execrows
UPDATE users
SET password_reset_token_hash = @token_hash, password_reset_requested_at = @requested_at
WHERE id = @id
  AND (password_reset_requested_at IS NULL OR password_reset_requested_at < @resend_cutoff);

-- name: ClearPasswordResetToken :exec
UPDATE users SET password_reset_token_hash = NULL, password_reset_requested_at = NULL WHERE id = $1;

-- name: ResetPasswordWithToken :one
UPDATE users
SET password = @password,
    password_reset_token_hash = NULL,
    password_reset_requested_at = NULL,
    email_verified = true,
    verification_token = NULL,
    verification_token_expires_at = NULL
WHERE password_reset_token_hash = @token_hash
  AND password_reset_requested_at > @issued_after
RETURNING id, is_coach;

-- name: GetPendingCoaches :many
SELECT * FROM users WHERE is_coach = true AND coach_validated = false AND email_verified = true ORDER BY created_at ASC;

-- name: ValidateCoach :exec
UPDATE users SET coach_validated = true WHERE id = $1 AND is_coach = true;

-- name: RejectCoach :exec
UPDATE users SET is_coach = false, coach_validated = false WHERE id = $1 AND is_coach = true;

-- name: SetVerificationToken :exec
UPDATE users SET verification_token = $2, verification_token_expires_at = $3, verification_email_sent_at = NOW() WHERE id = $1;

-- name: GetUserByVerificationToken :one
SELECT * FROM users WHERE verification_token = $1 AND verification_token_expires_at > NOW();

-- name: VerifyUserEmail :exec
UPDATE users SET email_verified = true, verification_token = NULL, verification_token_expires_at = NULL WHERE id = $1;

-- name: ClaimAccountNotice :execrows
UPDATE users
SET account_notice_sent_at = NOW()
WHERE id = @id
  AND (account_notice_sent_at IS NULL OR account_notice_sent_at < @resend_cutoff);

-- name: ListAllUsers :many
SELECT * FROM users ORDER BY created_at DESC;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
