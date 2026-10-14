package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"auth-api/internal/auth"
	"auth-api/internal/database"
	"auth-api/pkg/session"

	"golang.org/x/crypto/bcrypt"
)

// dummyHash is compared against when the email is unknown, so response timing
// does not reveal whether an account exists. It is a valid bcrypt hash at
// DefaultCost, computed once at startup.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)

// sessionTTL is how long a login stays valid in Redis.
const sessionTTL = 7 * 24 * time.Hour

// Login throttling: at most 30 attempts per IP and 5 failed attempts per
// account within this window.
const loginWindow = 15 * time.Minute

const invalidCredentialsMsg = "invalid email or password"

// Login verifies email + password and returns a signed access token.
// Only active, email-verified accounts can log in.
func Login(w http.ResponseWriter, r *http.Request) {
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
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email, ok := parseEmail(input.Email)
	if !ok || input.Password == "" || len(input.Password) > 72 {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	lim := newLimiter()
	acctKey := "login:acct:" + auth.HashToken(email)
	if lim.hit(ctx, w, "login:ip:"+clientIP(r), 30, loginWindow) ||
		lim.blocked(ctx, w, acctKey, 5, loginWindow) {
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("login: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	var (
		userID, hash, displayName, plan, status string
		verified                                bool
	)
	err = db.QueryRowContext(ctx, `
		SELECT id::text, password_hash, display_name, plan, status, email_verified_at IS NOT NULL
		FROM public.users
		WHERE email = $1
	`, email).Scan(&userID, &hash, &displayName, &plan, &status, &verified)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(input.Password))
		lim.fail(ctx, acctKey, loginWindow)
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	case err != nil:
		log.Printf("login: look up user: %v", err)
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password)) != nil {
		lim.fail(ctx, acctKey, loginWindow)
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	}

	lim.clear(ctx, acctKey)

	// Checked only after the password is proven correct, so these messages
	// cannot be used to discover accounts.
	if !verified || status == statusPendingVerification {
		writeError(w, http.StatusForbidden, "email not verified")
		return
	}
	if status != statusActive {
		writeError(w, http.StatusForbidden, "account is not active")
		return
	}

	// For domain-mode institutions with roster enforcement enabled, update
	// institutional entitlement only after the verified email matches the roster.
	rosterPlan, rosterErr := applyDomainRosterAccess(ctx, db, userID, email)
	if rosterErr != nil {
		log.Printf("login: roster authorization check: %v", rosterErr)
		writeError(w, http.StatusServiceUnavailable, "could not verify institution access")
		return
	}
	if rosterPlan != "" {
		plan = rosterPlan
	}

	// The raw token goes to the client; only its hash is stored in Redis.
	token, err := auth.GenerateSessionToken()
	if err != nil {
		log.Printf("login: generate session token: %v", err)
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}
	redis, err := session.NewRedis()
	if err != nil {
		log.Printf("login: redis unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}
	expiresAt := time.Now().UTC().Add(sessionTTL)
	if err := redis.CreateSession(ctx, auth.HashSessionToken(token), userID, sessionTTL); err != nil {
		log.Printf("login: store session: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_at":   expiresAt.Format(time.RFC3339),
		"user": map[string]string{
			"id":           userID,
			"email":        email,
			"display_name": displayName,
			"plan":         plan,
		},
	})
}
