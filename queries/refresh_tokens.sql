-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
VALUES (@user_id, @token_hash, @expires_at)
RETURNING *;

-- name: GetRefreshTokenByHash :one
SELECT * FROM refresh_tokens WHERE token_hash = @token_hash;

-- name: LockRefreshTokenByHash :one
SELECT * FROM refresh_tokens WHERE token_hash = @token_hash FOR UPDATE;

-- name: LockRefreshTokenByID :one
SELECT * FROM refresh_tokens WHERE id = @id FOR UPDATE;

-- The first rotation's time is kept, so presenting the token again inside the
-- grace does not stretch the grace.
-- name: RotateRefreshToken :exec
UPDATE refresh_tokens
SET revoked = true, revoked_at = COALESCE(revoked_at, now()), replaced_by = @replaced_by
WHERE id = @id;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked = true, revoked_at = COALESCE(revoked_at, now()), replaced_by = NULL
WHERE id = @id;

-- Locked oldest first. A successor is always created after its predecessor
-- committed, so this is the order Refresh and Logout lock a token and its
-- successor in, and the three cannot wait on each other in a cycle.
-- name: LockUserRefreshTokens :exec
SELECT id FROM refresh_tokens WHERE user_id = @user_id ORDER BY created_at, id FOR UPDATE;

-- name: RevokeUserRefreshTokens :exec
UPDATE refresh_tokens
SET revoked = true, revoked_at = COALESCE(revoked_at, now()), replaced_by = NULL
WHERE user_id = @user_id;

-- name: DeleteExpiredRefreshTokens :exec
DELETE FROM refresh_tokens WHERE expires_at < now();
