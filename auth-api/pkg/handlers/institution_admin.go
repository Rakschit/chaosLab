package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

const planInstitution = "institution"

var (
	slugPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)
	domainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

	// Difficulties that may have a monthly limit.
	limitLevels = []string{"easy", "medium", "hard"}
)

const defaultLimitsJSON = `{"easy":200,"medium":150,"hard":100}`

// licensedSQL is true when the institution in column `inst` of table alias `u`
// is active and has an active, unexpired license. It is the single definition
// of "this institution plan is valid right now".
const licensedSQL = `(u.institution_id IS NOT NULL AND EXISTS (
	SELECT 1
	FROM public.institutions i
	JOIN public.institution_licenses l ON l.institution_id = i.id
	WHERE i.id = u.institution_id
	  AND i.status = 'active'
	  AND l.status = 'active'
	  AND (l.expires_at IS NULL OR l.expires_at > NOW())
))`

type institutionInput struct {
	Name          string           `json:"name"`
	Slug          string           `json:"slug"`
	AuthMode      string           `json:"auth_mode"`
	LoginURL      string           `json:"login_url"`
	PublicKey     string           `json:"public_key"`
	MonthlyLimits map[string]int64 `json:"monthly_limits"`
	MaxSeats      int              `json:"max_seats"`
	ContactEmail  string           `json:"contact_email"`
}

type institutionRow struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Slug          string          `json:"slug"`
	Status        string          `json:"status"`
	AuthMode      string          `json:"auth_mode"`
	LoginURL      string          `json:"login_url"`
	HasPublicKey  bool            `json:"has_public_key"`
	MonthlyLimits json.RawMessage `json:"monthly_limits"`
	MaxSeats      int             `json:"max_seats"`
	ContactEmail  string          `json:"contact_email"`
	Domains       []string        `json:"domains"`
	LicenseActive bool            `json:"license_active"`
	LicenseExpiry *time.Time      `json:"license_expires_at"`
	CreatedAt     time.Time       `json:"created_at"`
}

// validateInstitution checks and normalises the fields of a new institution.
// It returns the limits as JSON, ready to store.
func validateInstitution(in *institutionInput) (limitsJSON string, errMsg string) {
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 2 || n > 120 {
		return "", "name must be 2 to 120 characters"
	}
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	if !slugPattern.MatchString(in.Slug) {
		return "", "slug must be 2 to 40 characters: lowercase letters, digits and dashes"
	}
	if in.AuthMode == "" {
		in.AuthMode = "domain"
	}
	if in.AuthMode != "domain" && in.AuthMode != "token" {
		return "", "auth_mode must be domain or token"
	}

	in.LoginURL = strings.TrimSpace(in.LoginURL)
	if in.AuthMode == "token" {
		u, err := url.Parse(in.LoginURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || len(in.LoginURL) > 500 {
			return "", "login_url must be an https URL when auth_mode is token"
		}
	} else if in.LoginURL != "" {
		return "", "login_url is only used when auth_mode is token"
	}

	in.PublicKey = strings.TrimSpace(in.PublicKey)
	if len(in.PublicKey) > 4096 {
		return "", "public_key is too long"
	}

	if in.MaxSeats < 0 || in.MaxSeats > 1_000_000 {
		return "", "max_seats must be between 0 and 1000000"
	}

	in.ContactEmail = strings.ToLower(strings.TrimSpace(in.ContactEmail))
	if in.ContactEmail != "" {
		if _, ok := parseEmail(in.ContactEmail); !ok {
			return "", "contact_email is not a valid email address"
		}
	}

	if in.MonthlyLimits == nil {
		return defaultLimitsJSON, ""
	}
	clean := make(map[string]int64, len(limitLevels))
	for _, level := range limitLevels {
		v, ok := in.MonthlyLimits[level]
		if !ok {
			return "", "monthly_limits needs easy, medium and hard"
		}
		if v < 0 || v > 100000 {
			return "", "each monthly limit must be between 0 and 100000"
		}
		clean[level] = v
	}
	if len(in.MonthlyLimits) != len(limitLevels) {
		return "", "monthly_limits may only contain easy, medium and hard"
	}
	b, _ := json.Marshal(clean)
	return string(b), ""
}

// AdminInstitutions lists institutions (GET) or creates one (POST). Admin only.
func AdminInstitutions(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	switch r.Method {
	case http.MethodGet:
		listInstitutions(w, r)
	case http.MethodPost:
		createInstitution(w, r)
	default:
		methodNotAllowed(w, "GET, POST, OPTIONS")
	}
}

func listInstitutions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	db, _, ok := adminUser(ctx, w, r)
	if !ok {
		return
	}

	rows, err := db.QueryContext(ctx, `
		SELECT i.id::text, i.name, i.slug, i.status, i.auth_mode, i.login_url,
		       i.public_key <> '', i.monthly_limits::text, i.max_seats, i.contact_email,
		       COALESCE((SELECT array_to_string(array_agg(d.domain ORDER BY d.domain), ',')
		                 FROM public.institution_domains d WHERE d.institution_id = i.id), ''),
		       EXISTS (SELECT 1 FROM public.institution_licenses l
		               WHERE l.institution_id = i.id AND l.status = 'active'
		                 AND (l.expires_at IS NULL OR l.expires_at > NOW())),
		       (SELECT MAX(l.expires_at) FROM public.institution_licenses l
		        WHERE l.institution_id = i.id AND l.status = 'active'),
		       i.created_at
		FROM public.institutions i
		ORDER BY i.created_at DESC
		LIMIT 200
	`)
	if err != nil {
		log.Printf("admin institutions: query: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load institutions")
		return
	}
	defer rows.Close()

	list := []institutionRow{}
	for rows.Next() {
		var it institutionRow
		var limits, domains string
		var expiry sql.NullTime
		if err := rows.Scan(&it.ID, &it.Name, &it.Slug, &it.Status, &it.AuthMode, &it.LoginURL,
			&it.HasPublicKey, &limits, &it.MaxSeats, &it.ContactEmail,
			&domains, &it.LicenseActive, &expiry, &it.CreatedAt); err != nil {
			log.Printf("admin institutions: scan: %v", err)
			writeError(w, http.StatusInternalServerError, "could not load institutions")
			return
		}
		it.MonthlyLimits = json.RawMessage(limits)
		it.Domains = []string{}
		if domains != "" {
			it.Domains = strings.Split(domains, ",")
		}
		if expiry.Valid {
			t := expiry.Time
			it.LicenseExpiry = &t
		}
		list = append(list, it)
	}
	if err := rows.Err(); err != nil {
		log.Printf("admin institutions: rows: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load institutions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"institutions": list})
}

func createInstitution(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	db, _, ok := adminUser(ctx, w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	defer r.Body.Close()
	var in institutionInput
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	limits, msg := validateInstitution(&in)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	var id string
	err := db.QueryRowContext(ctx, `
		INSERT INTO public.institutions
			(name, slug, auth_mode, login_url, public_key, monthly_limits, max_seats, contact_email)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)
		RETURNING id::text
	`, in.Name, in.Slug, in.AuthMode, in.LoginURL, in.PublicKey, limits, in.MaxSeats, in.ContactEmail).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		writeError(w, http.StatusConflict, "an institution with this slug already exists")
		return
	}
	if err != nil {
		log.Printf("admin institutions: insert: %v", err)
		writeError(w, http.StatusInternalServerError, "could not create institution")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": "Institution created. Add a domain and a license next.",
		"id":      id,
	})
}

// AdminInstitutionDomain adds an email domain to an institution. Admin only.
func AdminInstitutionDomain(w http.ResponseWriter, r *http.Request) {
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

	db, _, ok := adminUser(ctx, w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	defer r.Body.Close()
	var in struct {
		InstitutionID string `json:"institution_id"`
		Domain        string `json:"domain"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	domain := strings.ToLower(strings.TrimSpace(in.Domain))
	if !uuidPattern.MatchString(in.InstitutionID) || !domainPattern.MatchString(domain) {
		writeError(w, http.StatusBadRequest, "institution_id and a valid domain such as sicsr.ac.in are required")
		return
	}
	// Public mailbox providers must never grant an institution plan.
	if isPublicEmailDomain(domain) {
		writeError(w, http.StatusBadRequest, "public email providers cannot be used as an institution domain")
		return
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO public.institution_domains (domain, institution_id)
		VALUES ($1, $2::uuid)
	`, domain, in.InstitutionID)
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation:
		writeError(w, http.StatusConflict, "this domain already belongs to an institution")
		return
	case errors.As(err, &pgErr) && pgErr.Code == "23503": // foreign key
		writeError(w, http.StatusNotFound, "institution not found")
		return
	case err != nil:
		log.Printf("admin institution domain: insert: %v", err)
		writeError(w, http.StatusInternalServerError, "could not add domain")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"message": "Domain added.", "domain": domain})
}

// AdminInstitutionLicense creates or ends a license for an institution. Admin only.
// Action "grant" adds an active license (optionally with an expiry);
// action "revoke" ends every active license of that institution.
func AdminInstitutionLicense(w http.ResponseWriter, r *http.Request) {
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

	db, _, ok := adminUser(ctx, w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	defer r.Body.Close()
	var in struct {
		InstitutionID string  `json:"institution_id"`
		Action        string  `json:"action"`
		LicenseEmail  string  `json:"license_email"`
		ExpiresAt     *string `json:"expires_at"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !uuidPattern.MatchString(in.InstitutionID) {
		writeError(w, http.StatusBadRequest, "invalid institution_id")
		return
	}

	switch in.Action {
	case "revoke":
		res, err := db.ExecContext(ctx, `
			UPDATE public.institution_licenses SET status = 'revoked'
			WHERE institution_id = $1::uuid AND status = 'active'
		`, in.InstitutionID)
		if err != nil {
			log.Printf("admin institution license: revoke: %v", err)
			writeError(w, http.StatusInternalServerError, "could not revoke license")
			return
		}
		n, _ := res.RowsAffected()
		writeJSON(w, http.StatusOK, map[string]any{"message": "License revoked.", "revoked": n})

	case "grant":
		email, ok := parseEmail(in.LicenseEmail)
		if !ok {
			writeError(w, http.StatusBadRequest, "license_email must be a valid email address")
			return
		}
		var expires any // nil means no expiry
		if in.ExpiresAt != nil && strings.TrimSpace(*in.ExpiresAt) != "" {
			t, err := time.Parse(time.RFC3339, strings.TrimSpace(*in.ExpiresAt))
			if err != nil || !t.After(time.Now()) {
				writeError(w, http.StatusBadRequest, "expires_at must be a future time like 2027-06-30T00:00:00Z")
				return
			}
			expires = t.UTC()
		}

		var id string
		err := db.QueryRowContext(ctx, `
			INSERT INTO public.institution_licenses (institution_id, license_email, status, expires_at)
			VALUES ($1::uuid, $2, 'active', $3)
			RETURNING id::text
		`, in.InstitutionID, email, expires).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			writeError(w, http.StatusNotFound, "institution not found")
			return
		}
		if err != nil {
			log.Printf("admin institution license: insert: %v", err)
			writeError(w, http.StatusInternalServerError, "could not create license")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"message": "License active.", "id": id})

	default:
		writeError(w, http.StatusBadRequest, "action must be grant or revoke")
	}
}
