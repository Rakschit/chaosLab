package core

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

// validSeedPattern matches the shape a generated seed always has: 16 hex
// characters (8 random bytes, hex-encoded).
var validSeedPattern = regexp.MustCompile(`^[0-9a-fA-F]{16}$`)

// GenerateSecureSeed creates a fresh, unpredictable 16-character hex seed.
func GenerateSecureSeed() (string, error) {
	b := make([]byte, 8)
	if _, err := cryptorand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ResolveSeed validates a user-supplied seed and normalizes it to
// lowercase. If seed is empty or malformed, a fresh random seed is
// generated instead — callers never need to special-case "missing" vs
// "invalid".
func ResolveSeed(seed string) (string, error) {
	if seed != "" && validSeedPattern.MatchString(seed) {
		return strings.ToLower(seed), nil
	}
	return GenerateSecureSeed()
}
