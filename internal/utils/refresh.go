package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// RefreshTokenTTL is the lifetime of a refresh token.
const RefreshTokenTTL = 90 * 24 * time.Hour

// RefreshTokenReuseGrace is how long after a rotation the rotated token may be
// presented again, for a client that never received the answer: one swiped
// away mid refresh, or whose connection dropped on the way back.
const RefreshTokenReuseGrace = time.Minute

// GenerateRefreshToken returns a new random opaque refresh token.
func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hex digest of a token. Only the hash is stored.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
