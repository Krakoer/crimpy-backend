package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// RefreshTokenTTL is the lifetime of a refresh token.
const RefreshTokenTTL = 90 * 24 * time.Hour

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
