package auth

const PasswordResetTokenBytes = 32

// GeneratePasswordResetToken returns a cryptographically random token.
// Send it by email only, and store just HashPasswordResetToken(token).
func GeneratePasswordResetToken() (string, error) {
	return randomHex(PasswordResetTokenBytes, "password reset token")
}

// HashPasswordResetToken returns the SHA-256 hex digest of a raw reset token.
func HashPasswordResetToken(token string) string {
	return HashToken(token)
}
