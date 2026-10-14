package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"auth-api/internal/auth"
	"auth-api/internal/database"
	"auth-api/internal/services"
	"auth-api/pkg/session"

	"golang.org/x/crypto/bcrypt"
)

const genericForgotMsg = "If an account exists for that email, a password reset link has been sent."

// issuePasswordResetToken revokes outstanding reset tokens for the user and
// stores a fresh 30-minute one. The raw token must only be sent by email.
func issuePasswordResetToken(ctx context.Context, q queryer, userID string) (string, error) {
	rawToken, err := auth.GeneratePasswordResetToken()
	if err != nil {
		return "", err
	}
	if _, err := q.ExecContext(ctx, `
		UPDATE public.password_reset_tokens
		SET used_at = NOW()
		WHERE user_id = $1::uuid AND used_at IS NULL
	`, userID); err != nil {
		return "", err
	}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO public.password_reset_tokens (user_id, token_hash, expires_at)
		VALUES ($1::uuid, $2, NOW() + INTERVAL '30 minutes')
	`, userID, auth.HashPasswordResetToken(rawToken)); err != nil {
		return "", err
	}
	return rawToken, nil
}

// ForgotPassword emails a reset link to an active account. Every outcome that
// is not a malformed or rate-limited request returns the same 202, so the
// endpoint cannot be used to discover which emails have accounts.
func ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}

	lim := newLimiter()
	if lim.hit(r.Context(), w, "forgot:ip:"+clientIP(r), 10, time.Hour) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var input struct {
		Email string `json:"email"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "valid email is required")
		return
	}
	email, ok := parseEmail(input.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "valid email is required")
		return
	}
	if lim.hit(r.Context(), w, "forgot:email:"+auth.HashToken(email), 3, time.Hour) {
		return
	}

	respondAccepted := func() {
		writeJSON(w, http.StatusAccepted, map[string]string{"message": genericForgotMsg})
	}

	emailService, err := services.NewEmailService()
	if err != nil {
		log.Printf("forgot-password: email service unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("forgot-password: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("forgot-password: begin tx: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}
	defer func() { _ = tx.Rollback() }()

	var userID, displayName string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, display_name
		FROM public.users
		WHERE email = $1 AND status = 'active' AND email_verified_at IS NOT NULL
		FOR UPDATE
	`, email).Scan(&userID, &displayName)
	if errors.Is(err, sql.ErrNoRows) {
		respondAccepted()
		return
	}
	if err != nil {
		log.Printf("forgot-password: look up user: %v", err)
		writeError(w, http.StatusInternalServerError, "could not process request")
		return
	}

	// One reset email per account per minute.
	var recent bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM public.password_reset_tokens
			WHERE user_id = $1::uuid AND created_at > NOW() - INTERVAL '60 seconds'
		)`, userID).Scan(&recent); err != nil {
		log.Printf("forgot-password: cooldown check: %v", err)
		writeError(w, http.StatusInternalServerError, "could not process request")
		return
	}
	if recent {
		respondAccepted()
		return
	}

	rawToken, err := issuePasswordResetToken(ctx, tx, userID)
	if err != nil {
		log.Printf("forgot-password: issue token: %v", err)
		writeError(w, http.StatusInternalServerError, "could not process request")
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("forgot-password: commit: %v", err)
		writeError(w, http.StatusInternalServerError, "could not process request")
		return
	}

	if err := emailService.SendPasswordResetEmail(ctx, email, displayName, rawToken); err != nil {
		// Same response either way, so existence is not leaked.
		log.Printf("forgot-password: send email: %v", err)
	}
	respondAccepted()
}

// ResetPassword consumes a valid one-time reset token, sets the new password
// and signs the user out everywhere by deleting all of their Redis sessions.
func ResetPassword(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}
	if newLimiter().hit(r.Context(), w, "reset:ip:"+clientIP(r), 20, time.Hour) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil || strings.TrimSpace(input.Token) == "" {
		writeError(w, http.StatusBadRequest, "reset token is required")
		return
	}
	if len(input.Password) < 12 || len(input.Password) > 72 {
		writeError(w, http.StatusBadRequest, "password must be between 12 and 72 bytes")
		return
	}
	tokenHash := auth.HashPasswordResetToken(strings.TrimSpace(input.Token))

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("reset-password: bcrypt failed: %v", err)
		writeError(w, http.StatusInternalServerError, "could not reset password")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("reset-password: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("reset-password: begin tx: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}
	defer func() { _ = tx.Rollback() }()

	var userID string
	err = tx.QueryRowContext(ctx, `
		SELECT user_id::text
		FROM public.password_reset_tokens
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW()
		FOR UPDATE
	`, tokenHash).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "reset link is invalid or has expired")
		return
	}
	if err != nil {
		log.Printf("reset-password: look up token: %v", err)
		writeError(w, http.StatusInternalServerError, "could not reset password")
		return
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE public.users
		SET password_hash = $2, updated_at = NOW()
		WHERE id = $1::uuid AND status = 'active' AND email_verified_at IS NOT NULL
	`, userID, string(passwordHash))
	if err != nil {
		log.Printf("reset-password: update password: %v", err)
		writeError(w, http.StatusInternalServerError, "could not reset password")
		return
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		writeError(w, http.StatusBadRequest, "reset link is invalid or has expired")
		return
	}

	// Burn every outstanding reset token for the user.
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.password_reset_tokens
		SET used_at = NOW()
		WHERE user_id = $1::uuid AND used_at IS NULL
	`, userID); err != nil {
		log.Printf("reset-password: consume tokens: %v", err)
		writeError(w, http.StatusInternalServerError, "could not reset password")
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("reset-password: commit: %v", err)
		writeError(w, http.StatusInternalServerError, "could not reset password")
		return
	}

	// Sign the user out everywhere. The password is already changed, so a
	// failure here is logged rather than reported as a failed reset.
	if rds, err := session.NewRedis(); err != nil {
		log.Printf("reset-password: redis unavailable, sessions not revoked: %v", err)
	} else if err := rds.DeleteUserSessions(ctx, userID); err != nil {
		log.Printf("reset-password: revoke sessions: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Password updated. You can now log in."})
}
