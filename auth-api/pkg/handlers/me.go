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
	"auth-api/pkg/session"
)

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(h) < 8 || !strings.EqualFold(h[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

// sessionUserID resolves a raw session token to a user ID via Redis.
// It returns session.ErrSessionNotFound when the session is missing or expired.
func sessionUserID(ctx context.Context, rawToken string) (string, error) {
	redis, err := session.NewRedis()
	if err != nil {
		return "", err
	}
	return redis.Get(ctx, auth.HashSessionToken(rawToken))
}

// requireUser authenticates the request and returns the user's ID. On failure
// it has already written the error response and ok is false. Use it at the top
// of every protected endpoint.
func requireUser(ctx context.Context, w http.ResponseWriter, r *http.Request) (string, bool) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	userID, err := sessionUserID(ctx, token)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return "", false
		}
		log.Printf("auth: session lookup: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return "", false
	}
	return userID, true
}

// Me returns the logged-in user's profile.
func Me(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET, OPTIONS")
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("me: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	var email, displayName, plan, role, institutionID string
	var licensed bool
	err = db.QueryRowContext(ctx, `
		SELECT u.email, u.display_name, u.plan, u.role,
		       COALESCE(u.institution_id::text, ''),
		       `+licensedSQL+`
		FROM public.users u
		WHERE u.id = $1::uuid AND u.status = 'active' AND u.email_verified_at IS NOT NULL
	`, userID).Scan(&email, &displayName, &plan, &role, &institutionID, &licensed)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}
	if err != nil {
		log.Printf("me: look up user: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load profile")
		return
	}

	// The institution plan only counts while the institution is active and has
	// a valid license; otherwise the user falls back to free without any
	// change to their stored row.
	user := map[string]any{
		"id":           userID,
		"email":        email,
		"display_name": displayName,
		"role":         role,
	}
	if plan == planInstitution {
		if licensed {
			var name, limits, roleRaw, externalID string
			err := db.QueryRowContext(ctx, `
				SELECT name, monthly_limits::text, role_limits::text FROM public.institutions WHERE id = $1::uuid
			`, institutionID).Scan(&name, &limits, &roleRaw)
			if err == nil {
				err = db.QueryRowContext(ctx, `
					SELECT COALESCE(external_id, '') FROM public.users WHERE id = $1::uuid
				`, userID).Scan(&externalID)
			}
			if err != nil {
				log.Printf("me: load institution: %v", err)
				writeError(w, http.StatusInternalServerError, "could not load profile")
				return
			}
			// The limits sent on are this person's own: the institution's
			// ceiling lowered by the admin's limits for their role.
			memberRole := memberRoleOf(ctx, db, institutionID, email, externalID)
			effective, err := json.Marshal(effectiveLimits(parseLimits(limits), parseRoleLimits(roleRaw), memberRole))
			if err != nil {
				writeError(w, http.StatusInternalServerError, "could not load profile")
				return
			}
			user["institution"] = map[string]any{
				"id":             institutionID,
				"name":           name,
				"member_role":    memberRole,
				"monthly_limits": json.RawMessage(effective),
			}
		} else {
			plan = "free"
		}
	}
	user["plan"] = plan

	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}
