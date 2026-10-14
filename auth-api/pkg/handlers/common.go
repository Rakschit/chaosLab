package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/mail"
	"os"
	"strings"

	"auth-api/internal/auth"
)

const (
	statusPendingVerification = "pending_verification"
	statusActive              = "active"

	// recentTokenQuery enforces a minimum gap (60s) between verification emails
	// for the same account. It is derived from existing token rows, so it needs
	// no extra storage.
	recentTokenQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM public.email_verification_tokens
			WHERE user_id = $1::uuid AND created_at > NOW() - INTERVAL '60 seconds'
		)`

	genericResendMessage = "If an unverified account exists for that email, a verification email will be sent."
	genericRegisterMsg   = "If this email can be registered, a verification link has been sent. Check your email."
)

// queryer is satisfied by both *sql.DB and *sql.Tx.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// parseEmail normalises an address and rejects anything that is not a bare
// "local@domain" value. mail.ParseAddress alone also accepts forms such as
// "<a@b.com>" or "Name <a@b.com>", which must not be stored as-is.
func parseEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 {
		return "", false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", false
	}
	return email, true
}

// applyCORS adds CORS headers when CORS_ALLOWED_ORIGIN is set. The variable
// may hold several origins separated by commas, for example
// "http://127.0.0.1:5500,http://localhost:5500,https://app.example.com".
// It returns true when the request was a preflight that has been fully
// answered. With the variable unset, nothing changes.
func applyCORS(w http.ResponseWriter, r *http.Request) bool {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGIN"))
	if raw == "" {
		return false
	}

	w.Header().Add("Vary", "Origin")
	origin := r.Header.Get("Origin")
	for _, allowed := range strings.Split(raw, ",") {
		allowed = strings.TrimRight(strings.TrimSpace(allowed), "/")
		if allowed != "" && origin == allowed {
			w.Header().Set("Access-Control-Allow-Origin", allowed)
			break
		}
	}

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// hasRecentToken reports whether a verification token was issued very recently.
func hasRecentToken(ctx context.Context, q queryer, userID string) (bool, error) {
	var recent bool
	err := q.QueryRowContext(ctx, recentTokenQuery, userID).Scan(&recent)
	return recent, err
}

// issueVerificationToken revokes any outstanding tokens for the user and stores
// a fresh one. It returns the raw token, which must only be sent by email.
func issueVerificationToken(ctx context.Context, q queryer, userID string) (string, error) {
	rawToken, err := auth.GenerateVerificationToken()
	if err != nil {
		return "", err
	}
	if _, err := q.ExecContext(ctx, `
		UPDATE public.email_verification_tokens
		SET used_at = NOW()
		WHERE user_id = $1::uuid AND used_at IS NULL
	`, userID); err != nil {
		return "", err
	}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO public.email_verification_tokens (user_id, token_hash, expires_at)
		VALUES ($1::uuid, $2, NOW() + INTERVAL '1 hour')
	`, userID, auth.HashVerificationToken(rawToken)); err != nil {
		return "", err
	}
	return rawToken, nil
}
