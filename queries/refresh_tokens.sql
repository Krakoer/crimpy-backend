-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
VALUES (@user_id, @token_hash, @expires_at)
RETURNING *;

-- name: GetRefreshTokenByHash :one
SELECT * FROM refresh_tokens WHERE token_hash = @token_hash;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens SET revoked = true WHERE id = @id;

-- name: RevokeUserRefreshTokens :exec
UPDATE refresh_tokens SET revoked = true WHERE user_id = @user_id;

-- name: DeleteExpiredRefreshTokens :exec
DELETE FROM refresh_tokens WHERE expires_at < now();
