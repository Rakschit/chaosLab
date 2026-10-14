package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"auth-api/internal/auth"
	"auth-api/internal/database"
	"auth-api/internal/institution"
	"auth-api/pkg/session"
)

const (
	// stateTTL is how long a login attempt may take at the institution.
	stateTTL = 10 * time.Minute

	institutionAuthWindow = 15 * time.Minute

	// defaultAudience is the "aud" claim institutions must put in their tokens.
	// Override it with INSTITUTION_AUDIENCE.
	defaultAudience = "cyber-sim"

	// unusablePassword is not a bcrypt hash, so no password ever matches it:
	// participants can only sign in through their institution.
	unusablePassword = "!institution-login-only"
)

func institutionAudience() string {
	if a := strings.TrimSpace(os.Getenv("INSTITUTION_AUDIENCE")); a != "" {
		return a
	}
	return defaultAudience
}

// activeTokenInstitutionSQL selects one institution that can accept token
// logins right now: active, "token" method, and holding an active license.
const activeTokenInstitutionSQL = `
	SELECT i.id::text, i.slug, i.name, i.login_url, i.public_key, i.max_seats
	FROM public.institutions i
	WHERE i.status = 'active'
	  AND i.auth_mode = 'token'
	  AND i.login_url <> '' AND i.public_key <> ''
	  AND EXISTS (
	      SELECT 1 FROM public.institution_licenses l
	      WHERE l.institution_id = i.id AND l.status = 'active'
	        AND (l.expires_at IS NULL OR l.expires_at > NOW()))
`

type tokenInstitution struct {
	ID, Slug, Name, LoginURL, PublicKey string
	MaxSeats                            int
}

func scanInstitution(row *sql.Row) (tokenInstitution, error) {
	var in tokenInstitution
	err := row.Scan(&in.ID, &in.Slug, &in.Name, &in.LoginURL, &in.PublicKey, &in.MaxSeats)
	return in, err
}

// InstitutionStart begins a login through an institution. The caller sends
// either the institution's slug or an email address on one of its domains; the
// response is the URL of the institution's own login page, carrying a
// single-use "state" value.
func InstitutionStart(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	defer r.Body.Close()
	var input struct {
		Institution string `json:"institution"` // slug, or an email address
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	query := strings.ToLower(strings.TrimSpace(input.Institution))
	if query == "" || len(query) > 254 {
		writeError(w, http.StatusBadRequest, "institution is required")
		return
	}

	callback := strings.TrimSpace(os.Getenv("INSTITUTION_CALLBACK_URL"))
	if callback == "" {
		log.Printf("institution start: INSTITUTION_CALLBACK_URL is not set")
		writeError(w, http.StatusServiceUnavailable, "institution login is not configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if newLimiter().hit(ctx, w, "inst:start:"+clientIP(r), 30, institutionAuthWindow) {
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("institution start: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	var inst tokenInstitution
	if at := strings.LastIndex(query, "@"); at >= 0 {
		domain := query[at+1:]
		inst, err = scanInstitution(db.QueryRowContext(ctx,
			activeTokenInstitutionSQL+` AND i.id = (SELECT d.institution_id FROM public.institution_domains d WHERE d.domain = $1)`,
			domain))
	} else {
		inst, err = scanInstitution(db.QueryRowContext(ctx,
			activeTokenInstitutionSQL+` AND i.slug = $1`, query))
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no institution login is available for that address")
		return
	}
	if err != nil {
		log.Printf("institution start: look up: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start login")
		return
	}

	state, err := auth.GenerateSessionToken()
	if err != nil {
		log.Printf("institution start: state: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start login")
		return
	}
	rds, err := session.NewRedis()
	if err != nil {
		log.Printf("institution start: redis unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}
	if err := rds.PutOnce(ctx, auth.HashToken(state), inst.ID, stateTTL); err != nil {
		log.Printf("institution start: store state: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	u, err := url.Parse(inst.LoginURL)
	if err != nil || u.Scheme != "https" {
		log.Printf("institution start: bad login_url for %s", inst.Slug)
		writeError(w, http.StatusInternalServerError, "institution login is misconfigured")
		return
	}
	q := u.Query()
	q.Set("state", state)
	q.Set("audience", institutionAudience())
	q.Set("redirect_uri", callback)
	u.RawQuery = q.Encode()

	writeJSON(w, http.StatusOK, map[string]string{
		"redirect_url": u.String(),
		"institution":  inst.Name,
	})
}

// InstitutionCallback completes a login. The institution has authenticated the
// person and sent back a signed token; this verifies it and signs the person in
// with a normal session. Only an opaque ID (and an optional nickname) is kept.
func InstitutionCallback(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	defer r.Body.Close()
	var input struct {
		Token string `json:"token"`
		State string `json:"state"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil || input.Token == "" || input.State == "" || len(input.State) > 128 {
		writeError(w, http.StatusBadRequest, "token and state are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if newLimiter().hit(ctx, w, "inst:cb:"+clientIP(r), 30, institutionAuthWindow) {
		return
	}

	// The state is single-use: taking it removes it, so a replayed callback
	// (or a token injected into someone else's browser) finds nothing.
	rds, err := session.NewRedis()
	if err != nil {
		log.Printf("institution callback: redis unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}
	instID, err := rds.TakeOnce(ctx, auth.HashToken(input.State))
	if err != nil {
		log.Printf("institution callback: take state: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}
	const loginFailed = "login failed or expired, please start again"
	if instID == "" {
		writeError(w, http.StatusBadRequest, loginFailed)
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("institution callback: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	// Re-check the institution and its license now, not only at start.
	inst, err := scanInstitution(db.QueryRowContext(ctx,
		activeTokenInstitutionSQL+` AND i.id = $1::uuid`, instID))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "this institution's plan is not active")
		return
	}
	if err != nil {
		log.Printf("institution callback: look up institution: %v", err)
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}

	claims, err := institution.Verify(input.Token, inst.PublicKey, inst.Slug, institutionAudience(), input.State, time.Now())
	if err != nil {
		log.Printf("institution callback: token rejected for %s", inst.Slug)
		writeError(w, http.StatusUnauthorized, loginFailed)
		return
	}

	// Once an institute imports a roster, token-mode access requires an active
	// roster row matching the verified opaque subject. Never trust client IDs.
	var rosterEnforced bool
	if err := db.QueryRowContext(ctx, `SELECT roster_enforced FROM public.institutions WHERE id=$1::uuid`, inst.ID).Scan(&rosterEnforced); err != nil {
		log.Printf("institution callback: roster setting: %v", err)
		writeError(w, http.StatusInternalServerError, "could not verify institution access")
		return
	}
	if rosterEnforced {
		var allowed bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.institution_roster WHERE institution_id=$1::uuid AND external_id=$2 AND active=TRUE)`, inst.ID, claims.Subject).Scan(&allowed); err != nil {
			log.Printf("institution callback: roster check: %v", err)
			writeError(w, http.StatusInternalServerError, "could not verify institution access")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "your institution has not authorized this account")
			return
		}
	}

	userID, displayName, code := upsertParticipant(ctx, db, inst, claims)
	switch code {
	case http.StatusOK:
	case http.StatusForbidden:
		writeError(w, http.StatusForbidden, "this account is not allowed to sign in, or the institution has no free seats")
		return
	default:
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}

	token, err := auth.GenerateSessionToken()
	if err != nil {
		log.Printf("institution callback: generate session token: %v", err)
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}
	expiresAt := time.Now().UTC().Add(sessionTTL)
	if err := rds.CreateSession(ctx, auth.HashSessionToken(token), userID, sessionTTL); err != nil {
		log.Printf("institution callback: store session: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_at":   expiresAt.Format(time.RFC3339),
		"user": map[string]string{
			"id":           userID,
			"display_name": displayName,
			"plan":         planInstitution,
			"institution":  inst.Name,
		},
	})
}

// upsertParticipant finds or creates the minimal user row for this person. It
// returns the user's ID and display name and an HTTP-style code: 200 on
// success, 403 if the person is blocked or no seat is free, 500 on errors.
func upsertParticipant(ctx context.Context, db *sql.DB, inst tokenInstitution, c institution.Claims) (id, name string, code int) {
	var status string
	err := db.QueryRowContext(ctx, `
		SELECT id::text, display_name, status
		FROM public.users
		WHERE institution_id = $1::uuid AND external_id = $2 AND account_type = 'participant'
	`, inst.ID, c.Subject).Scan(&id, &name, &status)

	switch {
	case err == nil:
		if status != statusActive {
			return "", "", http.StatusForbidden
		}
		if c.Name != "" && c.Name != name {
			if _, err := db.ExecContext(ctx, `
				UPDATE public.users SET display_name = $2, updated_at = NOW() WHERE id = $1::uuid
			`, id, c.Name); err != nil {
				log.Printf("institution callback: update name: %v", err)
			} else {
				name = c.Name
			}
		}
		return id, name, http.StatusOK

	case !errors.Is(err, sql.ErrNoRows):
		log.Printf("institution callback: look up participant: %v", err)
		return "", "", http.StatusInternalServerError
	}

	// New participant: enforce the seat limit, then create the row.
	if inst.MaxSeats > 0 {
		var used int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM public.users WHERE institution_id = $1::uuid AND account_type = 'participant'`, inst.ID).Scan(&used); err != nil {
			log.Printf("institution callback: count seats: %v", err)
			return "", "", http.StatusInternalServerError
		}
		if used >= inst.MaxSeats {
			return "", "", http.StatusForbidden
		}
	}

	name = c.Name
	if name == "" {
		name = "Participant"
	}
	sum := sha256.Sum256([]byte(inst.ID + "|" + c.Subject))
	email := "p-" + hex.EncodeToString(sum[:20]) + "@participants.invalid"

	err = db.QueryRowContext(ctx, `
		INSERT INTO public.users
			(email, password_hash, display_name, plan, status, email_verified_at,
			 institution_id, external_id, account_type)
		VALUES ($1, $2, $3, 'institution', 'active', NOW(), $4::uuid, $5, 'participant')
		ON CONFLICT (institution_id, external_id) WHERE external_id IS NOT NULL
		DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`, email, unusablePassword, name, inst.ID, c.Subject).Scan(&id)
	if err != nil {
		log.Printf("institution callback: create participant: %v", err)
		return "", "", http.StatusInternalServerError
	}
	return id, name, http.StatusOK
}
