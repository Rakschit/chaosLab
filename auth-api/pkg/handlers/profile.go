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
	"unicode"
	"unicode/utf8"

	"auth-api/internal/database"
)

// validateDisplayName trims the name and applies the same length rule as
// registration, plus a ban on control characters (newlines, tabs, ...).
func validateDisplayName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return "", false
		}
	}
	return name, true
}

// UpdateProfile lets the logged-in user change their display name (username).
func UpdateProfile(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return
	}
	if newLimiter().hit(ctx, w, "profile:user:"+userID, 20, time.Hour) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var input struct {
		DisplayName string `json:"display_name"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, ok := validateDisplayName(input.DisplayName)
	if !ok {
		writeError(w, http.StatusBadRequest, "username is required and must be at most 100 characters, with no control characters")
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("profile: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	var email, displayName, plan string
	err = db.QueryRowContext(ctx, `
		UPDATE public.users
		SET display_name = $2, updated_at = NOW()
		WHERE id = $1::uuid AND status = 'active' AND email_verified_at IS NOT NULL
		RETURNING email, display_name, plan
	`, userID, name).Scan(&email, &displayName, &plan)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}
	if err != nil {
		log.Printf("profile: update display name: %v", err)
		writeError(w, http.StatusInternalServerError, "could not update username")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "Username updated.",
		"user": map[string]string{
			"id":           userID,
			"email":        email,
			"display_name": displayName,
			"plan":         plan,
		},
	})
}
