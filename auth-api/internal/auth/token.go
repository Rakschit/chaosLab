package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const SessionTokenBytes = 32

// randomHex returns n cryptographically secure random bytes, hex encoded.
func randomHex(n int, what string) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("failed to generate " + what)
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hex digest of a raw token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GenerateSessionToken creates a cryptographically secure random token.
// Return the raw token to the client and store only its hash.
func GenerateSessionToken() (string, error) {
	return randomHex(SessionTokenBytes, "session token")
}

// HashSessionToken returns the SHA-256 hex digest of a raw session token.
func HashSessionToken(token string) string {
	return HashToken(token)
}
