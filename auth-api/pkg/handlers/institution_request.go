package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"

	"auth-api/internal/database"
	"auth-api/internal/services"
)

const (
	maxRequestDomains  = 5
	maxSeatsWanted     = 1_000_000
	defaultLicenseDays = 365
)

// publicEmailDomains must never grant an institution plan: anyone can have an
// address there.
var publicEmailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "yahoo.com": true, "yahoo.co.in": true,
	"outlook.com": true, "hotmail.com": true, "live.com": true, "msn.com": true,
	"icloud.com": true, "me.com": true, "aol.com": true, "proton.me": true,
	"protonmail.com": true, "rediffmail.com": true, "gmx.com": true, "mail.com": true,
}

func isPublicEmailDomain(domain string) bool {
	return publicEmailDomains[domain]
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// cleanLine trims a single-line text and enforces a length range (in
// characters) and a ban on control characters.
func cleanLine(raw string, min, max int) (string, bool) {
	s := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(s); n < min || n > max {
		return "", false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return "", false
		}
	}
	return s, true
}

// emailDomain returns the part after the last "@", lowercased.
func emailDomain(email string) string {
	return strings.ToLower(email[strings.LastIndex(email, "@")+1:])
}

// splitDomains turns the comma-joined form used in SQL back into a slice.
func splitDomains(csv string) []string {
	if csv == "" {
		return []string{}
	}
	return strings.Split(csv, ",")
}

// normalizeDomains cleans a list of requested domains. The second result is a
// user-facing error message, or "" when the list is fine.
func normalizeDomains(raw []string) ([]string, string) {
	if len(raw) == 0 || len(raw) > maxRequestDomains {
		return nil, fmt.Sprintf("give between 1 and %d email domains", maxRequestDomains)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, d := range raw {
		d = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d)), "@")
		if !domainPattern.MatchString(d) {
			return nil, fmt.Sprintf("%q is not a valid domain, use a form like college.edu", d)
		}
		if isPublicEmailDomain(d) {
			return nil, "public email providers cannot be used as an institution domain"
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out, ""
}

// withSlugSuffix appends a short random suffix, keeping the slug within 40 characters.
func withSlugSuffix(slug string) string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	if len(slug) > 34 {
		slug = slug[:34]
	}
	return strings.TrimRight(slug, "-") + "-" + hex.EncodeToString(b)
}

// slugFromName makes a first guess at an institution code from its name.
func slugFromName(name string) string {
	s := strings.Trim(nonSlugChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 30 {
		s = strings.TrimRight(s[:30], "-")
	}
	if len(s) < 2 {
		return withSlugSuffix("inst")
	}
	return s
}

// parseLicenseExpiry accepts "" (default: one year from now), a date such as
// 2027-06-30 (valid through the end of that day, UTC) or an RFC 3339 time.
func parseLicenseExpiry(raw string) (time.Time, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC().AddDate(0, 0, defaultLicenseDays), ""
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		d, derr := time.Parse("2006-01-02", raw)
		if derr != nil {
			return time.Time{}, "license_expires_at must be a date like 2027-06-30"
		}
		t = d.Add(24*time.Hour - time.Second)
	}
	t = t.UTC()
	if !t.After(time.Now()) {
		return time.Time{}, "license_expires_at must be in the future"
	}
	return t, ""
}

type institutionRequestJSON struct {
	ID              string     `json:"id"`
	Status          string     `json:"status"`
	InstitutionName string     `json:"institution_name"`
	Domains         []string   `json:"domains"`
	SeatsWanted     int        `json:"seats_wanted"`
	Message         string     `json:"message"`
	DecisionNote    string     `json:"decision_note"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedAt       *time.Time `json:"decided_at"`
}

// InstitutionRequest handles the logged-in user's own institution request:
// GET returns the latest one, POST creates a new one.
func InstitutionRequest(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	switch r.Method {
	case http.MethodGet:
		getInstitutionRequest(w, r)
	case http.MethodPost:
		createInstitutionRequest(w, r)
	default:
		methodNotAllowed(w, "GET, POST, OPTIONS")
	}
}

func getInstitutionRequest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("institution-request: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	var rq institutionRequestJSON
	var domains string
	var decidedAt sql.NullTime
	err = db.QueryRowContext(ctx, `
		SELECT id::text, status, institution_name, array_to_string(domains, ','),
		       seats_wanted, message, decision_note, created_at, decided_at
		FROM public.institution_requests
		WHERE user_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT 1
	`, userID).Scan(&rq.ID, &rq.Status, &rq.InstitutionName, &domains,
		&rq.SeatsWanted, &rq.Message, &rq.DecisionNote, &rq.CreatedAt, &decidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"request": nil})
		return
	}
	if err != nil {
		log.Printf("institution-request: load: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load your request")
		return
	}
	rq.Domains = splitDomains(domains)
	if decidedAt.Valid {
		rq.DecidedAt = &decidedAt.Time
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": rq})
}

func createInstitutionRequest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return
	}
	if newLimiter().hit(ctx, w, "instreq:user:"+userID, 5, time.Hour) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var input struct {
		InstitutionName string   `json:"institution_name"`
		Domains         []string `json:"domains"`
		SeatsWanted     int      `json:"seats_wanted"`
		Message         string   `json:"message"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name, ok := cleanLine(input.InstitutionName, 2, 120)
	if !ok {
		writeError(w, http.StatusBadRequest, "institution name must be 2 to 120 characters")
		return
	}
	domains, msg := normalizeDomains(input.Domains)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if input.SeatsWanted < 0 || input.SeatsWanted > maxSeatsWanted {
		writeError(w, http.StatusBadRequest, "seats must be between 0 and 1000000")
		return
	}
	message, ok := cleanText(input.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "message must be at most 500 characters, with no control characters")
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("institution-request: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	var email, displayName, accountType string
	var hasInstitution bool
	err = db.QueryRowContext(ctx, `
		SELECT email, display_name, account_type, institution_id IS NOT NULL
		FROM public.users
		WHERE id = $1::uuid AND status = 'active' AND email_verified_at IS NOT NULL
	`, userID).Scan(&email, &displayName, &accountType, &hasInstitution)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}
	if err != nil {
		log.Printf("institution-request: look up user: %v", err)
		writeError(w, http.StatusInternalServerError, "could not send request")
		return
	}
	if accountType != "user" {
		writeError(w, http.StatusForbidden, "institution accounts cannot request an institution plan")
		return
	}
	if hasInstitution {
		writeError(w, http.StatusConflict, "you already belong to an institution")
		return
	}

	// Evidence of ownership: the requester's own verified email must be on one
	// of the domains. The admin still reviews every request by hand.
	own := emailDomain(email)
	matches := false
	for _, d := range domains {
		if d == own {
			matches = true
			break
		}
	}
	if !matches {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("one of the domains must match your account email (@%s)", own))
		return
	}

	var taken string
	err = db.QueryRowContext(ctx, `
		SELECT domain FROM public.institution_domains
		WHERE domain = ANY(string_to_array($1::text, ','))
		LIMIT 1
	`, strings.Join(domains, ",")).Scan(&taken)
	switch {
	case err == nil:
		writeError(w, http.StatusConflict, fmt.Sprintf("%s already belongs to an institution", taken))
		return
	case !errors.Is(err, sql.ErrNoRows):
		log.Printf("institution-request: check domains: %v", err)
		writeError(w, http.StatusInternalServerError, "could not send request")
		return
	}

	var rq institutionRequestJSON
	err = db.QueryRowContext(ctx, `
		INSERT INTO public.institution_requests (user_id, institution_name, domains, seats_wanted, message)
		VALUES ($1::uuid, $2, string_to_array($3::text, ','), $4, $5)
		RETURNING id::text, status, institution_name, seats_wanted, message, decision_note, created_at
	`, userID, name, strings.Join(domains, ","), input.SeatsWanted, message).Scan(
		&rq.ID, &rq.Status, &rq.InstitutionName, &rq.SeatsWanted, &rq.Message, &rq.DecisionNote, &rq.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			writeError(w, http.StatusConflict, "you already have a pending request")
			return
		}
		log.Printf("institution-request: insert: %v", err)
		writeError(w, http.StatusInternalServerError, "could not send request")
		return
	}
	rq.Domains = domains

	// Best effort: the request is saved even if the email cannot be sent.
	notifyAdminOfInstitutionRequest(ctx, email, displayName, name, domains, input.SeatsWanted, message)

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": "Request sent. You will get an email when it is reviewed.",
		"request": rq,
	})
}

func notifyAdminOfInstitutionRequest(ctx context.Context, userEmail, displayName, institutionName string, domains []string, seats int, message string) {
	adminEmail := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))
	if adminEmail == "" {
		log.Printf("institution-request: ADMIN_EMAIL is not set, admin was not notified")
		return
	}
	svc, err := services.NewEmailService()
	if err != nil {
		log.Printf("institution-request: email service unavailable: %v", err)
		return
	}
	if err := svc.SendInstitutionRequestAdminEmail(ctx, adminEmail, userEmail, displayName, institutionName, domains, seats, message); err != nil {
		log.Printf("institution-request: admin email failed: %v", err)
	}
}

// AdminInstitutionRequests lists pending institution requests (admin only).
func AdminInstitutionRequests(w http.ResponseWriter, r *http.Request) {
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

	db, _, ok := adminUser(ctx, w, r)
	if !ok {
		return
	}

	rows, err := db.QueryContext(ctx, `
		SELECT ir.id::text, ir.institution_name, array_to_string(ir.domains, ','),
		       ir.seats_wanted, ir.message, ir.created_at, u.email, u.display_name
		FROM public.institution_requests ir
		JOIN public.users u ON u.id = ir.user_id
		WHERE ir.status = 'pending'
		ORDER BY ir.created_at ASC
		LIMIT 100
	`)
	if err != nil {
		log.Printf("admin institution requests: query: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load requests")
		return
	}
	defer rows.Close()

	type item struct {
		ID              string    `json:"id"`
		InstitutionName string    `json:"institution_name"`
		Domains         []string  `json:"domains"`
		SeatsWanted     int       `json:"seats_wanted"`
		Message         string    `json:"message"`
		CreatedAt       time.Time `json:"created_at"`
		Email           string    `json:"email"`
		DisplayName     string    `json:"display_name"`
	}
	list := []item{}
	for rows.Next() {
		var it item
		var domains string
		if err := rows.Scan(&it.ID, &it.InstitutionName, &domains, &it.SeatsWanted,
			&it.Message, &it.CreatedAt, &it.Email, &it.DisplayName); err != nil {
			log.Printf("admin institution requests: scan: %v", err)
			writeError(w, http.StatusInternalServerError, "could not load requests")
			return
		}
		it.Domains = splitDomains(domains)
		list = append(list, it)
	}
	if err := rows.Err(); err != nil {
		log.Printf("admin institution requests: rows: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load requests")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"requests": list})
}

// AdminInstitutionRequestDecision approves or rejects one pending request
// (admin only). Approving, in one transaction: creates the institution (domain
// method), registers its domains, grants a license, and moves already
// verified free users on those domains onto the institution plan (up to the
// seat limit). Users who verify their email later join automatically, as before.
func AdminInstitutionRequestDecision(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	db, adminID, ok := adminUser(ctx, w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var input struct {
		ID               string           `json:"id"`
		Decision         string           `json:"decision"`
		Note             string           `json:"note"`
		Slug             string           `json:"slug"`
		MaxSeats         *int             `json:"max_seats"`
		MonthlyLimits    map[string]int64 `json:"monthly_limits"`
		LicenseExpiresAt string           `json:"license_expires_at"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !uuidPattern.MatchString(input.ID) {
		writeError(w, http.StatusBadRequest, "invalid request id")
		return
	}
	if input.Decision != "approve" && input.Decision != "reject" {
		writeError(w, http.StatusBadRequest, "decision must be approve or reject")
		return
	}
	note, ok := cleanText(input.Note)
	if !ok {
		writeError(w, http.StatusBadRequest, "note must be at most 500 characters, with no control characters")
		return
	}

	if input.Decision == "reject" {
		var userID, name, domainsCSV string
		err := db.QueryRowContext(ctx, `
			UPDATE public.institution_requests
			SET status = 'rejected', decision_note = $2, decided_by = $3::uuid, decided_at = NOW()
			WHERE id = $1::uuid AND status = 'pending'
			RETURNING user_id::text, institution_name, array_to_string(domains, ',')
		`, input.ID, note, adminID).Scan(&userID, &name, &domainsCSV)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusConflict, "request not found or already decided")
			return
		}
		if err != nil {
			log.Printf("admin institution decision: reject: %v", err)
			writeError(w, http.StatusInternalServerError, "could not save decision")
			return
		}
		notifyRequesterOfInstitutionDecision(ctx, db, userID, name, splitDomains(domainsCSV), false, note)
		writeJSON(w, http.StatusOK, map[string]string{"message": "Request rejected."})
		return
	}

	// ---- approve ----
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("admin institution decision: begin: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}
	defer tx.Rollback()

	var userID, name, domainsCSV string
	var seatsWanted int
	err = tx.QueryRowContext(ctx, `
		UPDATE public.institution_requests
		SET status = 'approved', decision_note = $2, decided_by = $3::uuid, decided_at = NOW()
		WHERE id = $1::uuid AND status = 'pending'
		RETURNING user_id::text, institution_name, array_to_string(domains, ','), seats_wanted
	`, input.ID, note, adminID).Scan(&userID, &name, &domainsCSV, &seatsWanted)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusConflict, "request not found or already decided")
		return
	}
	if err != nil {
		log.Printf("admin institution decision: update request: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}
	domains := splitDomains(domainsCSV)

	var contact string
	if err := tx.QueryRowContext(ctx,
		`SELECT email FROM public.users WHERE id = $1::uuid`, userID).Scan(&contact); err != nil {
		log.Printf("admin institution decision: load requester: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}

	maxSeats := seatsWanted
	if input.MaxSeats != nil {
		maxSeats = *input.MaxSeats
	}
	slug := strings.ToLower(strings.TrimSpace(input.Slug))
	explicitSlug := slug != ""
	if !explicitSlug {
		slug = slugFromName(name)
	}
	in := institutionInput{
		Name:          name,
		Slug:          slug,
		AuthMode:      "domain",
		MaxSeats:      maxSeats,
		ContactEmail:  contact,
		MonthlyLimits: input.MonthlyLimits,
	}
	limits, msg := validateInstitution(&in)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	expires, msg := parseLicenseExpiry(input.LicenseExpiresAt)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	// ON CONFLICT DO NOTHING (not an error) keeps the transaction usable when
	// a generated code is already taken.
	var instID string
	for attempt := 0; attempt < 5 && instID == ""; attempt++ {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO public.institutions (name, slug, auth_mode, monthly_limits, max_seats, contact_email)
			VALUES ($1, $2, 'domain', $3::jsonb, $4, $5)
			ON CONFLICT (slug) DO NOTHING
			RETURNING id::text
		`, in.Name, in.Slug, limits, in.MaxSeats, in.ContactEmail).Scan(&instID)
		if err == nil {
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("admin institution decision: insert institution: %v", err)
			writeError(w, http.StatusInternalServerError, "could not create institution")
			return
		}
		if explicitSlug {
			writeError(w, http.StatusConflict, "an institution with this code already exists")
			return
		}
		in.Slug = withSlugSuffix(in.Slug)
	}
	if instID == "" {
		writeError(w, http.StatusInternalServerError, "could not pick a unique institution code, type one in by hand")
		return
	}

	for _, d := range domains {
		if isPublicEmailDomain(d) {
			writeError(w, http.StatusBadRequest, "public email providers cannot be used as an institution domain")
			return
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO public.institution_domains (domain, institution_id)
			VALUES ($1, $2::uuid)
			ON CONFLICT (domain) DO NOTHING
		`, d, instID)
		if err != nil {
			log.Printf("admin institution decision: insert domain: %v", err)
			writeError(w, http.StatusInternalServerError, "could not add domain")
			return
		}
		if n, _ := res.RowsAffected(); n != 1 {
			writeError(w, http.StatusConflict, fmt.Sprintf("%s already belongs to an institution", d))
			return
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO public.institution_licenses (institution_id, license_email, status, expires_at)
		VALUES ($1::uuid, $2, 'active', $3)
	`, instID, contact, expires); err != nil {
		log.Printf("admin institution decision: insert license: %v", err)
		writeError(w, http.StatusInternalServerError, "could not create license")
		return
	}

	// Move people who already verified their email before approval. Pro users
	// stay on Pro, like the join at email verification. NULL limit = no seat cap.
	var seatCap any
	if in.MaxSeats > 0 {
		seatCap = in.MaxSeats
	}
	res, err := tx.ExecContext(ctx, `
		WITH picked AS (
			SELECT u.id FROM public.users u
			WHERE u.plan = 'free' AND u.account_type = 'user' AND u.status = 'active'
			  AND u.email_verified_at IS NOT NULL
			  AND lower(split_part(u.email, '@', 2)) = ANY(string_to_array($2::text, ','))
			ORDER BY u.id
			LIMIT $3::int
		)
		UPDATE public.users u
		SET institution_id = $1::uuid, plan = 'institution', updated_at = NOW()
		FROM picked
		WHERE u.id = picked.id
	`, instID, strings.Join(domains, ","), seatCap)
	if err != nil {
		log.Printf("admin institution decision: move users: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}
	moved, _ := res.RowsAffected()

	if _, err := tx.ExecContext(ctx, `
		UPDATE public.institution_requests SET institution_id = $2::uuid WHERE id = $1::uuid
	`, input.ID, instID); err != nil {
		log.Printf("admin institution decision: link request: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("admin institution decision: commit: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}

	// Best effort: the decision is already saved.
	notifyRequesterOfInstitutionDecision(ctx, db, userID, name, domains, true, note)

	writeJSON(w, http.StatusOK, map[string]any{
		"message": fmt.Sprintf("Approved. Institution code %s created; %d existing user(s) moved onto the plan.", in.Slug, moved),
		"slug":    in.Slug,
		"moved":   moved,
	})
}

func notifyRequesterOfInstitutionDecision(ctx context.Context, db *sql.DB, userID, institutionName string, domains []string, approved bool, note string) {
	var email, displayName string
	err := db.QueryRowContext(ctx, `
		SELECT email, display_name FROM public.users WHERE id = $1::uuid
	`, userID).Scan(&email, &displayName)
	if err != nil {
		log.Printf("admin institution decision: load user for email: %v", err)
		return
	}
	svc, err := services.NewEmailService()
	if err != nil {
		log.Printf("admin institution decision: email service unavailable: %v", err)
		return
	}
	if err := svc.SendInstitutionDecisionEmail(ctx, email, displayName, institutionName, domains, approved, note); err != nil {
		log.Printf("admin institution decision: user email failed: %v", err)
	}
}
