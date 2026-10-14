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
	"unicode/utf8"

	"auth-api/internal/auth"
	"auth-api/internal/database"
	"auth-api/internal/services"

	"golang.org/x/crypto/bcrypt"
)

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// Register creates a pending free-plan account and sends an email verification link.
//
// The response is intentionally identical whether the address is new, already
// pending, or already registered, so the endpoint cannot be used to discover
// which emails have accounts. Re-registering a still-unverified address
// replaces its password and display name, so whoever controls the inbox and
// registers last is the one who ends up owning the account.
func Register(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}
	if newLimiter().hit(r.Context(), w, "register:ip:"+clientIP(r), 10, time.Hour) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var input registerRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email, ok := parseEmail(input.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "a valid email address is required")
		return
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" || utf8.RuneCountInString(displayName) > 100 {
		writeError(w, http.StatusBadRequest, "display_name is required and must be at most 100 characters")
		return
	}
	if len(input.Password) < 12 || len(input.Password) > 72 {
		writeError(w, http.StatusBadRequest, "password must be between 12 and 72 bytes")
		return
	}

	// Fail early on misconfiguration, before any account is created.
	emailService, err := services.NewEmailService()
	if err != nil {
		log.Printf("register: email service unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	// The hash is computed for every valid request, including ones for
	// addresses that already exist, so response timing does not reveal them.
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("register: bcrypt failed: %v", err)
		writeError(w, http.StatusInternalServerError, "could not process registration")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("register: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("register: begin tx: %v", err)
		writeError(w, http.StatusServiceUnavailable, "could not start registration")
		return
	}
	defer func() { _ = tx.Rollback() }()

	respondAccepted := func() {
		writeJSON(w, http.StatusCreated, map[string]any{
			"message":                     genericRegisterMsg,
			"email_verification_required": true,
		})
	}

	var (
		userID   string
		status   string
		verified bool
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, status, email_verified_at IS NOT NULL
		FROM public.users
		WHERE email = $1
		FOR UPDATE
	`, email).Scan(&userID, &status, &verified)

	switch {
	case err == nil:
		if verified || status != statusPendingVerification {
			// Existing, usable account: do nothing and reveal nothing.
			respondAccepted()
			return
		}
		recent, err := hasRecentToken(ctx, tx, userID)
		if err != nil {
			log.Printf("register: cooldown check: %v", err)
			writeError(w, http.StatusInternalServerError, "could not process registration")
			return
		}
		if recent {
			respondAccepted()
			return
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.users
			SET password_hash = $2, display_name = $3, updated_at = NOW()
			WHERE id = $1::uuid
		`, userID, string(passwordHash), displayName); err != nil {
			log.Printf("register: update pending user: %v", err)
			writeError(w, http.StatusInternalServerError, "could not process registration")
			return
		}

	case errors.Is(err, sql.ErrNoRows):
		err = tx.QueryRowContext(ctx, `
			INSERT INTO public.users (email, password_hash, display_name, plan, status, email_verified_at)
			VALUES ($1, $2, $3, 'free', 'pending_verification', NULL)
			RETURNING id::text
		`, email, string(passwordHash), displayName).Scan(&userID)
		if err != nil {
			if isUniqueViolation(err) {
				// Lost a race with a concurrent registration of the same email.
				respondAccepted()
				return
			}
			log.Printf("register: insert user: %v", err)
			writeError(w, http.StatusInternalServerError, "could not create account")
			return
		}

	default:
		log.Printf("register: lookup user: %v", err)
		writeError(w, http.StatusInternalServerError, "could not process registration")
		return
	}

	rawToken, err := issueVerificationToken(ctx, tx, userID)
	if err != nil {
		log.Printf("register: issue token: %v", err)
		writeError(w, http.StatusInternalServerError, "could not create verification request")
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("register: commit: %v", err)
		writeError(w, http.StatusInternalServerError, "could not finish registration")
		return
	}

	if err := emailService.SendVerificationEmail(ctx, email, displayName, rawToken); err != nil {
		// Same response as success so existence is not leaked; the user can
		// request another link through resend-verification.
		log.Printf("register: send verification email: %v", err)
	}
	respondAccepted()
}

// VerifyEmail consumes a valid one-time token and activates the account.
func VerifyEmail(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()

	var input struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Token) == "" {
		writeError(w, http.StatusBadRequest, "verification token is required")
		return
	}
	tokenHash := auth.HashToken(strings.TrimSpace(input.Token))

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("verify-email: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("verify-email: begin tx: %v", err)
		writeError(w, http.StatusServiceUnavailable, "verification unavailable")
		return
	}
	defer func() { _ = tx.Rollback() }()

	var userID string
	err = tx.QueryRowContext(ctx, `
		SELECT user_id::text
		FROM public.email_verification_tokens
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW()
		FOR UPDATE
	`, tokenHash).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "verification token is invalid or expired")
		return
	}
	if err != nil {
		log.Printf("verify-email: look up token: %v", err)
		writeError(w, http.StatusInternalServerError, "could not verify email")
		return
	}

	// Only accounts that are still pending are activated, so a valid token can
	// never reactivate a suspended or otherwise restricted account.
	res, err := tx.ExecContext(ctx, `
		UPDATE public.users
		SET email_verified_at = NOW(), status = 'active', updated_at = NOW()
		WHERE id = $1::uuid AND status = 'pending_verification'
	`, userID)
	if err != nil {
		log.Printf("verify-email: activate user: %v", err)
		writeError(w, http.StatusInternalServerError, "could not verify email")
		return
	}
	activated, err := res.RowsAffected()
	if err != nil {
		log.Printf("verify-email: rows affected: %v", err)
		writeError(w, http.StatusInternalServerError, "could not verify email")
		return
	}

	// Burn every outstanding token for the user, not just the one presented.
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.email_verification_tokens
		SET used_at = NOW()
		WHERE user_id = $1::uuid AND used_at IS NULL
	`, userID); err != nil {
		log.Printf("verify-email: consume tokens: %v", err)
		writeError(w, http.StatusInternalServerError, "could not complete verification")
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("verify-email: commit: %v", err)
		writeError(w, http.StatusInternalServerError, "could not complete verification")
		return
	}

	if activated == 0 {
		writeError(w, http.StatusBadRequest, "verification token is invalid or expired")
		return
	}
	// Best effort: the email is already verified, so a failure here only means
	// the user starts on the free plan.
	joinInstitutionByDomain(ctx, db, userID)

	writeJSON(w, http.StatusOK, map[string]string{"message": "Email verified successfully. You can now log in."})
}

// joinInstitutionByDomain puts a freshly verified free user on their
// institution's plan when the exact domain of their (now verified) email
// belongs to an active institution that uses the "domain" method, has an
// active license, and still has a free seat.
func joinInstitutionByDomain(ctx context.Context, db queryer, userID string) {
	res, err := db.ExecContext(ctx, `
		UPDATE public.users u
		SET institution_id = d.institution_id, plan = 'institution', updated_at = NOW()
		FROM public.institution_domains d
		JOIN public.institutions i ON i.id = d.institution_id
		WHERE u.id = $1::uuid
		  AND u.plan = 'free'
		  AND u.account_type = 'user'
		  AND d.domain = lower(split_part(u.email, '@', 2))
		  AND i.status = 'active'
		  AND i.auth_mode = 'domain'
		  AND EXISTS (
		      SELECT 1 FROM public.institution_licenses l
		      WHERE l.institution_id = i.id AND l.status = 'active'
		        AND (l.expires_at IS NULL OR l.expires_at > NOW()))
		  AND (i.max_seats = 0
		       OR (SELECT COUNT(*) FROM public.users x WHERE x.institution_id = i.id) < i.max_seats)
	`, userID)
	if err != nil {
		log.Printf("verify-email: join institution: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 1 {
		log.Printf("verify-email: user %s joined an institution", userID)
	}
}

// ResendVerification issues a fresh verification token for an unverified account.
// Every outcome that is not a malformed request returns the same 202 response,
// so the endpoint does not reveal whether the email exists.
func ResendVerification(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}
	if newLimiter().hit(r.Context(), w, "resend:ip:"+clientIP(r), 10, time.Hour) {
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

	respondAccepted := func() {
		writeJSON(w, http.StatusAccepted, map[string]string{"message": genericResendMessage})
	}

	emailService, err := services.NewEmailService()
	if err != nil {
		log.Printf("resend-verification: email service unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("resend-verification: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("resend-verification: begin tx: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}
	defer func() { _ = tx.Rollback() }()

	var userID, displayName string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, display_name
		FROM public.users
		WHERE email = $1 AND email_verified_at IS NULL AND status = 'pending_verification'
		FOR UPDATE
	`, email).Scan(&userID, &displayName)
	if errors.Is(err, sql.ErrNoRows) {
		respondAccepted()
		return
	}
	if err != nil {
		log.Printf("resend-verification: look up user: %v", err)
		writeError(w, http.StatusInternalServerError, "could not process verification request")
		return
	}

	recent, err := hasRecentToken(ctx, tx, userID)
	if err != nil {
		log.Printf("resend-verification: cooldown check: %v", err)
		writeError(w, http.StatusInternalServerError, "could not process verification request")
		return
	}
	if recent {
		respondAccepted()
		return
	}

	rawToken, err := issueVerificationToken(ctx, tx, userID)
	if err != nil {
		log.Printf("resend-verification: issue token: %v", err)
		writeError(w, http.StatusInternalServerError, "could not process verification request")
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("resend-verification: commit: %v", err)
		writeError(w, http.StatusInternalServerError, "could not process verification request")
		return
	}

	if err := emailService.SendVerificationEmail(ctx, email, displayName, rawToken); err != nil {
		// Do not surface this to the caller: a 5xx here would reveal that the
		// account exists. The cooldown lapses and the user can try again.
		log.Printf("resend-verification: send verification email: %v", err)
	}
	respondAccepted()
}

// pgx reports PostgreSQL unique violations with SQLSTATE 23505. Keep this helper
// dependency-free by inspecting the driver's error interface.
func isUniqueViolation(err error) bool {
	type sqlStater interface{ SQLState() string }
	var stateErr sqlStater
	if errors.As(err, &stateErr) {
		return stateErr.SQLState() == "23505"
	}
	return false
}
