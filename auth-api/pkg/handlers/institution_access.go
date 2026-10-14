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

	"github.com/jackc/pgx/v5/pgconn"

	"auth-api/internal/database"
)

// ---------------------------------------------------------------------------
// Per-role limits
// ---------------------------------------------------------------------------

var memberRoles = []string{"student", "faculty"}

func parseLimits(raw string) map[string]int64 {
	out := map[string]int64{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func parseRoleLimits(raw string) map[string]map[string]int64 {
	out := map[string]map[string]int64{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// effectiveLimits is what one member may play per month: the platform limits
// for the institution (the ceiling), lowered by the institution admin's limits
// for that member's role. A role limit can never raise the ceiling.
func effectiveLimits(ceiling map[string]int64, roleLimits map[string]map[string]int64, memberRole string) map[string]int64 {
	out := make(map[string]int64, len(ceiling))
	for level, max := range ceiling {
		v := max
		if rl, ok := roleLimits[memberRole][level]; ok && rl >= 0 && rl < v {
			v = rl
		}
		out[level] = v
	}
	return out
}

// memberRoleOf looks the person up on the institution's roster, by email hash
// or by external ID. Anyone not listed (for example someone who joined before
// the roster existed) counts as a student, the more restrictive default.
func memberRoleOf(ctx context.Context, db *sql.DB, institutionID, email, externalID string) string {
	hash, err := rosterEmailHash(email)
	if err != nil {
		hash = ""
	}
	var role string
	err = db.QueryRowContext(ctx, `
		SELECT roster_role
		FROM public.institution_roster
		WHERE institution_id = $1::uuid AND active = TRUE
		  AND ((email_hash IS NOT NULL AND email_hash = $2) OR (external_id IS NOT NULL AND external_id = $3 AND $3 <> ''))
		ORDER BY (email_hash = $2) DESC NULLS LAST
		LIMIT 1
	`, institutionID, hash, externalID).Scan(&role)
	if err != nil || (role != "student" && role != "faculty") {
		return "student"
	}
	return role
}

// InstitutionLimits lets the institution admin read and set the monthly limits
// for students and faculty. GET returns the ceiling and the current values;
// POST saves new ones.
func InstitutionLimits(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	db, actorID, institutionID, ok := institutionAdminUser(ctx, w, r)
	if !ok {
		return
	}

	var ceilingRaw, roleRaw string
	if err := db.QueryRowContext(ctx, `
		SELECT monthly_limits::text, role_limits::text FROM public.institutions WHERE id = $1::uuid
	`, institutionID).Scan(&ceilingRaw, &roleRaw); err != nil {
		log.Printf("institution limits: load: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load limits")
		return
	}
	ceiling := parseLimits(ceilingRaw)
	roleLimits := parseRoleLimits(roleRaw)

	switch r.Method {
	case http.MethodGet:
		current := map[string]map[string]int64{}
		for _, role := range memberRoles {
			current[role] = effectiveLimits(ceiling, roleLimits, role)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ceiling": ceiling, "limits": current})

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		defer r.Body.Close()
		var in struct {
			Student map[string]int64 `json:"student"`
			Faculty map[string]int64 `json:"faculty"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		next := map[string]map[string]int64{}
		for role, vals := range map[string]map[string]int64{"student": in.Student, "faculty": in.Faculty} {
			clean := map[string]int64{}
			for _, level := range limitLevels {
				v, present := vals[level]
				if !present {
					writeError(w, http.StatusBadRequest, role+" limit for "+level+" is missing")
					return
				}
				max, hasCeiling := ceiling[level]
				if v < 0 || (hasCeiling && v > max) {
					writeError(w, http.StatusBadRequest, role+" "+level+" limit must be between 0 and "+itoa(int(max))+" (the most your institution licence allows)")
					return
				}
				clean[level] = v
			}
			next[role] = clean
		}

		encoded, err := json.Marshal(next)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not save limits")
			return
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not save limits")
			return
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.institutions SET role_limits = $2::jsonb, updated_at = NOW() WHERE id = $1::uuid
		`, institutionID, string(encoded)); err != nil {
			log.Printf("institution limits: save: %v", err)
			writeError(w, http.StatusInternalServerError, "could not save limits")
			return
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO public.institution_audit (institution_id, actor_user_id, action, target_hash)
			VALUES ($1::uuid, $2::uuid, 'limits.update', '')
		`, institutionID, actorID); err != nil {
			log.Printf("institution limits: audit: %v", err)
			writeError(w, http.StatusInternalServerError, "could not save limits")
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save limits")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": "Limits saved. They apply to students and faculty from their next request."})

	default:
		methodNotAllowed(w, "GET, POST, OPTIONS")
	}
}

// ---------------------------------------------------------------------------
// Access requests: someone on the institution's domain who is not on the roster
// ---------------------------------------------------------------------------

type accessRequestJSON struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	Role          string     `json:"role"`
	Message       string     `json:"message"`
	DecisionNote  string     `json:"decision_note"`
	CreatedAt     time.Time  `json:"created_at"`
	DecidedAt     *time.Time `json:"decided_at"`
	InstitutionID string     `json:"-"`
}

// eligibleInstitution finds the institution whose domain matches the email,
// but only one that uses the domain method, is active, has a valid licence and
// enforces a roster. Only then does "not on the roster" mean anything.
func eligibleInstitution(ctx context.Context, db *sql.DB, email string) (id, name string, found bool, err error) {
	domain := emailDomain(email)
	if domain == "" {
		return "", "", false, nil
	}
	err = db.QueryRowContext(ctx, `
		SELECT i.id::text, i.name
		FROM public.institution_domains d
		JOIN public.institutions i ON i.id = d.institution_id
		WHERE d.domain = $1
		  AND i.status = 'active' AND i.auth_mode = 'domain' AND i.roster_enforced = TRUE
		  AND EXISTS (
		      SELECT 1 FROM public.institution_licenses l
		      WHERE l.institution_id = i.id AND l.status = 'active'
		        AND (l.expires_at IS NULL OR l.expires_at > NOW()))
		LIMIT 1
	`, domain).Scan(&id, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return id, name, true, nil
}

// InstitutionAccessRequest is the person's own side. GET says whether they can
// ask and shows their latest request; POST sends one to the institution admin.
func InstitutionAccessRequest(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w, "GET, POST, OPTIONS")
		return
	}
	if r.Method == http.MethodPost && newLimiter().hit(ctx, w, "instaccess:user:"+userID, 5, time.Hour) {
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("access-request: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	var email, plan, accountType, role string
	var hasInstitution bool
	err = db.QueryRowContext(ctx, `
		SELECT email, plan, account_type, role, institution_id IS NOT NULL
		FROM public.users
		WHERE id = $1::uuid AND status = 'active' AND email_verified_at IS NOT NULL
	`, userID).Scan(&email, &plan, &accountType, &role, &hasInstitution)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}
	if err != nil {
		log.Printf("access-request: look up user: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load your account")
		return
	}

	instID, instName := "", ""
	eligible := false
	if plan == "free" && accountType == "user" && role == "user" && !hasInstitution {
		id, name, found, err := eligibleInstitution(ctx, db, email)
		if err != nil {
			log.Printf("access-request: find institution: %v", err)
			writeError(w, http.StatusInternalServerError, "could not check your institution")
			return
		}
		instID, instName, eligible = id, name, found
	}

	if r.Method == http.MethodGet {
		out := map[string]any{"eligible": eligible, "request": nil}
		if eligible {
			out["institution"] = instName
		}
		var ar accessRequestJSON
		var decidedAt sql.NullTime
		err := db.QueryRowContext(ctx, `
			SELECT id::text, status, requested_role, message, decision_note, created_at, decided_at
			FROM public.institution_access_requests
			WHERE user_id = $1::uuid
			ORDER BY created_at DESC LIMIT 1
		`, userID).Scan(&ar.ID, &ar.Status, &ar.Role, &ar.Message, &ar.DecisionNote, &ar.CreatedAt, &decidedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("access-request: load: %v", err)
			writeError(w, http.StatusInternalServerError, "could not load your request")
			return
		}
		if err == nil {
			if decidedAt.Valid {
				ar.DecidedAt = &decidedAt.Time
			}
			out["request"] = ar
			// A request that was already approved or is pending is shown even
			// when the person is no longer eligible.
			if ar.Status == "pending" {
				out["eligible"] = true
			}
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	// POST
	if !eligible {
		writeError(w, http.StatusForbidden, "no institution with roster access matches your email")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var in struct {
		Role    string `json:"role"`
		Message string `json:"message"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	reqRole := strings.ToLower(strings.TrimSpace(in.Role))
	if reqRole != "student" && reqRole != "faculty" {
		writeError(w, http.StatusBadRequest, "role must be student or faculty")
		return
	}
	message, ok := cleanText(in.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "message must be at most 500 characters, with no control characters")
		return
	}

	var ar accessRequestJSON
	err = db.QueryRowContext(ctx, `
		INSERT INTO public.institution_access_requests (institution_id, user_id, requested_role, message)
		VALUES ($1::uuid, $2::uuid, $3, $4)
		RETURNING id::text, status, requested_role, message, decision_note, created_at
	`, instID, userID, reqRole, message).Scan(&ar.ID, &ar.Status, &ar.Role, &ar.Message, &ar.DecisionNote, &ar.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			writeError(w, http.StatusConflict, "you already have a pending request")
			return
		}
		log.Printf("access-request: insert: %v", err)
		writeError(w, http.StatusInternalServerError, "could not send request")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"message":     "Request sent to the " + instName + " administrator.",
		"request":     ar,
		"institution": instName,
	})
}

// InstitutionAccessRequests lists the pending requests for the admin's own
// institution (institution admin only).
func InstitutionAccessRequests(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET, OPTIONS")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	db, _, institutionID, ok := institutionAdminUser(ctx, w, r)
	if !ok {
		return
	}
	rows, err := db.QueryContext(ctx, `
		SELECT ar.id::text, u.email, u.display_name, ar.requested_role, ar.message, ar.created_at
		FROM public.institution_access_requests ar
		JOIN public.users u ON u.id = ar.user_id
		WHERE ar.institution_id = $1::uuid AND ar.status = 'pending'
		ORDER BY ar.created_at ASC
		LIMIT 200
	`, institutionID)
	if err != nil {
		log.Printf("access requests: query: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load requests")
		return
	}
	defer rows.Close()

	type item struct {
		ID          string    `json:"id"`
		Email       string    `json:"email"`
		DisplayName string    `json:"display_name"`
		Role        string    `json:"role"`
		Message     string    `json:"message"`
		CreatedAt   time.Time `json:"created_at"`
	}
	list := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Email, &it.DisplayName, &it.Role, &it.Message, &it.CreatedAt); err != nil {
			log.Printf("access requests: scan: %v", err)
			writeError(w, http.StatusInternalServerError, "could not load requests")
			return
		}
		list = append(list, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load requests")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": list})
}

// InstitutionAccessDecision approves or rejects one request (institution admin
// only). Approving puts the person on the roster with the chosen role and moves
// them onto the institution plan, in one transaction.
func InstitutionAccessDecision(w http.ResponseWriter, r *http.Request) {
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

	db, adminID, institutionID, ok := institutionAdminUser(ctx, w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var in struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
		Role     string `json:"role"`
		Note     string `json:"note"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !uuidPattern.MatchString(in.ID) {
		writeError(w, http.StatusBadRequest, "invalid request id")
		return
	}
	var status string
	switch in.Decision {
	case "approve":
		status = "approved"
	case "reject":
		status = "rejected"
	default:
		writeError(w, http.StatusBadRequest, "decision must be approve or reject")
		return
	}
	note, ok := cleanText(in.Note)
	if !ok {
		writeError(w, http.StatusBadRequest, "note must be at most 500 characters, with no control characters")
		return
	}
	chosenRole := strings.ToLower(strings.TrimSpace(in.Role))
	if chosenRole != "" && chosenRole != "student" && chosenRole != "faculty" {
		writeError(w, http.StatusBadRequest, "role must be student or faculty")
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}
	defer func() { _ = tx.Rollback() }()

	var targetID, requestedRole string
	err = tx.QueryRowContext(ctx, `
		UPDATE public.institution_access_requests
		SET status = $3, decision_note = $4, decided_by = $5::uuid, decided_at = NOW()
		WHERE id = $1::uuid AND institution_id = $2::uuid AND status = 'pending'
		RETURNING user_id::text, requested_role
	`, in.ID, institutionID, status, note, adminID).Scan(&targetID, &requestedRole)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusConflict, "request not found or already decided")
		return
	}
	if err != nil {
		log.Printf("access decision: update: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}

	targetHash := ""
	if status == "approved" {
		role := chosenRole
		if role == "" {
			role = requestedRole
		}

		var email string
		if err := tx.QueryRowContext(ctx, `SELECT email FROM public.users WHERE id = $1::uuid`, targetID).Scan(&email); err != nil {
			log.Printf("access decision: load user: %v", err)
			writeError(w, http.StatusInternalServerError, "could not save decision")
			return
		}
		hash, err := rosterEmailHash(email)
		if err != nil {
			log.Printf("access decision: hash: %v", err)
			writeError(w, http.StatusServiceUnavailable, "roster hashing is not configured")
			return
		}
		targetHash = hash

		// Seats: 0 means unlimited. People already on the plan are not counted twice.
		var maxSeats, used int
		if err := tx.QueryRowContext(ctx, `
			SELECT i.max_seats,
			       (SELECT COUNT(*) FROM public.users x WHERE x.institution_id = i.id AND x.id <> $2::uuid)
			FROM public.institutions i WHERE i.id = $1::uuid
		`, institutionID, targetID).Scan(&maxSeats, &used); err != nil {
			log.Printf("access decision: seats: %v", err)
			writeError(w, http.StatusInternalServerError, "could not save decision")
			return
		}
		if maxSeats > 0 && used >= maxSeats {
			writeError(w, http.StatusConflict, "no free seats left for this institution")
			return
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO public.institution_roster (institution_id, email_hash, roster_role, active)
			VALUES ($1::uuid, $2, $3, TRUE)
			ON CONFLICT (institution_id, email_hash)
			DO UPDATE SET roster_role = EXCLUDED.roster_role, active = TRUE, updated_at = NOW()
		`, institutionID, hash, role); err != nil {
			log.Printf("access decision: roster: %v", err)
			writeError(w, http.StatusInternalServerError, "could not add to roster")
			return
		}

		// Free accounts move onto the plan now. Pro and admin accounts are only
		// added to the roster, like the other join paths.
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.users
			SET institution_id = $2::uuid, plan = 'institution', updated_at = NOW()
			WHERE id = $1::uuid AND plan = 'free' AND account_type = 'user' AND role = 'user'
		`, targetID, institutionID); err != nil {
			log.Printf("access decision: move user: %v", err)
			writeError(w, http.StatusInternalServerError, "could not save decision")
			return
		}
	}

	action := "access.reject"
	if status == "approved" {
		action = "access.approve"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO public.institution_audit (institution_id, actor_user_id, action, target_hash)
		VALUES ($1::uuid, $2::uuid, $3, $4)
	`, institutionID, adminID, action, targetHash); err != nil {
		log.Printf("access decision: audit: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}

	msg := "Request rejected."
	if status == "approved" {
		msg = "Request approved. They are on the roster and the institution plan."
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": msg})
}
