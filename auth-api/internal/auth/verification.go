package auth

const VerificationTokenBytes = 32

// GenerateVerificationToken returns a URL-safe, cryptographically random token.
// Store only HashVerificationToken(token) in the database.
func GenerateVerificationToken() (string, error) {
	return randomHex(VerificationTokenBytes, "verification token")
}

// HashVerificationToken returns the SHA-256 hex digest of a raw verification token.
func HashVerificationToken(token string) string {
	return HashToken(token)
}
