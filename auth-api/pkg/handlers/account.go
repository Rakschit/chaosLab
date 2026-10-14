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

const wrongPasswordMsg = "current password is incorrect"

// confirmPassword checks the caller's current password before a sensitive
// action (changing the password, deleting the account). Wrong guesses are
// throttled per user, so a stolen session token cannot be used to brute-force
// the password. On failure it has already written the response and returns
// false.
//
// A wrong password answers 403, not 401: the frontend treats 401 as "session
// expired" and would otherwise log the user out for a typo.
func confirmPassword(ctx context.Context, w http.ResponseWriter, lim *limiter, db *sql.DB, userID, password string) bool {
	if password == "" || len(password) > 72 {
		writeError(w, http.StatusBadRequest, "current password is required")
		return false
	}

	key := "pwconfirm:user:" + userID
	if lim.blocked(ctx, w, key, 5, loginWindow) {
		return false
	}

	var hash string
	err := db.QueryRowContext(ctx, `
		SELECT password_hash
		FROM public.users
		WHERE id = $1::uuid AND status = 'active' AND email_verified_at IS NOT NULL
	`, userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return false
	}
	if err != nil {
		log.Printf("account: look up password hash: %v", err)
		writeError(w, http.StatusInternalServerError, "could not verify password")
		return false
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		lim.fail(ctx, key, loginWindow)
		writeError(w, http.StatusForbidden, wrongPasswordMsg)
		return false
	}

	lim.clear(ctx, key)
	return true
}

// revokeAllSessions signs the user out everywhere. The token that made the
// request is also deleted directly, in case the per-user index is missing.
func revokeAllSessions(ctx context.Context, userID, rawToken, caller string) {
	rds, err := session.NewRedis()
	if err != nil {
		log.Printf("%s: redis unavailable, sessions not revoked: %v", caller, err)
		return
	}
	if err := rds.DeleteUserSessions(ctx, userID); err != nil {
		log.Printf("%s: revoke sessions: %v", caller, err)
	}
	if rawToken != "" {
		if err := rds.Delete(ctx, auth.HashSessionToken(rawToken)); err != nil {
			log.Printf("%s: delete current session: %v", caller, err)
		}
	}
}

// ChangePassword lets the logged-in user set a new password. It needs the
// current password, and signs the user out on every device afterwards, so the
// client must log in again with the new password.
func ChangePassword(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return
	}
	lim := newLimiter()
	if lim.hit(ctx, w, "chgpw:user:"+userID, 10, time.Hour) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Same rule as registration and password reset (bcrypt reads 72 bytes max).
	if len(input.NewPassword) < 12 || len(input.NewPassword) > 72 {
		writeError(w, http.StatusBadRequest, "password must be between 12 and 72 bytes")
		return
	}
	if input.NewPassword == input.CurrentPassword {
		writeError(w, http.StatusBadRequest, "new password must be different from the current one")
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("change-password: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	if !confirmPassword(ctx, w, lim, db, userID, input.CurrentPassword) {
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("change-password: bcrypt failed: %v", err)
		writeError(w, http.StatusInternalServerError, "could not change password")
		return
	}

	res, err := db.ExecContext(ctx, `
		UPDATE public.users
		SET password_hash = $2, updated_at = NOW()
		WHERE id = $1::uuid AND status = 'active' AND email_verified_at IS NOT NULL
	`, userID, string(newHash))
	if err != nil {
		log.Printf("change-password: update: %v", err)
		writeError(w, http.StatusInternalServerError, "could not change password")
		return
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}

	// Burn any reset links that were requested before the change.
	if _, err := db.ExecContext(ctx, `
		UPDATE public.password_reset_tokens
		SET used_at = NOW()
		WHERE user_id = $1::uuid AND used_at IS NULL
	`, userID); err != nil {
		log.Printf("change-password: consume reset tokens: %v", err)
	}

	// The password is already changed, so a revoke failure is only logged.
	revokeAllSessions(ctx, userID, bearerToken(r), "change-password")

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Password updated. Please log in again.",
	})
}

// DeleteAccount permanently deletes the logged-in user's account. It needs the
// current password. Verification and reset tokens are removed by their
// ON DELETE CASCADE foreign keys, and every session is revoked.
func DeleteAccount(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return
	}
	lim := newLimiter()
	if lim.hit(ctx, w, "delacct:user:"+userID, 10, time.Hour) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var input struct {
		Password string `json:"password"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("delete-account: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	if !confirmPassword(ctx, w, lim, db, userID, input.Password) {
		return
	}

	res, err := db.ExecContext(ctx, `DELETE FROM public.users WHERE id = $1::uuid`, userID)
	if err != nil {
		log.Printf("delete-account: delete user: %v", err)
		writeError(w, http.StatusInternalServerError, "could not delete account")
		return
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}

	// The account is gone; sessions must not outlive it. A failure here is
	// logged rather than reported, since the deletion itself succeeded.
	revokeAllSessions(ctx, userID, bearerToken(r), "delete-account")

	writeJSON(w, http.StatusOK, map[string]string{"message": "Your account has been deleted."})
}
