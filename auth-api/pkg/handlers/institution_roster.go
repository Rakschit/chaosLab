package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"auth-api/internal/database"
)

const rosterMaxRows = 10000

type rosterInput struct {
	Email      string `json:"email"`
	Name       string `json:"name"` // accepted for CSV compatibility; never persisted
	Role       string `json:"role"`
	ExternalID string `json:"external_id"`
}

// institutionAdminUser authenticates the caller and scopes all operations to
// the institution linked to that account in the database. No client-supplied
// institution ID is accepted for authorization.
func institutionAdminUser(ctx context.Context, w http.ResponseWriter, r *http.Request) (*sql.DB, string, string, bool) {
	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return nil, "", "", false
	}
	if newLimiter().hit(ctx, w, "institution-admin:"+userID, 120, time.Hour) {
		return nil, "", "", false
	}
	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("institution admin: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return nil, "", "", false
	}
	var institutionID, role string
	err = db.QueryRowContext(ctx, `
        SELECT u.institution_id::text, u.role
        FROM public.users u
        JOIN public.institutions i ON i.id = u.institution_id
        WHERE u.id = $1::uuid AND u.status = 'active'
          AND u.email_verified_at IS NOT NULL AND u.role = 'institution_admin'
          AND i.status = 'active'
    `, userID).Scan(&institutionID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "institution administrator access required")
		return nil, "", "", false
	}
	if err != nil {
		log.Printf("institution admin: permission lookup: %v", err)
		writeError(w, http.StatusInternalServerError, "could not check permissions")
		return nil, "", "", false
	}
	return db, userID, institutionID, true
}

func rosterEmailHash(email string) (string, error) {
	key := strings.TrimSpace(os.Getenv("INSTITUTION_ROSTER_HMAC_KEY"))
	if len(key) < 32 {
		return "", errors.New("INSTITUTION_ROSTER_HMAC_KEY must be at least 32 characters")
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// InstitutionRoster GETs roster status or POSTs a validated full roster import.
// The frontend parses CSV and sends rows as JSON; the server remains authoritative.
func InstitutionRoster(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	db, actorID, institutionID, ok := institutionAdminUser(ctx, w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		var enforced bool
		var students, faculty int
		if err := db.QueryRowContext(ctx, `SELECT roster_enforced FROM public.institutions WHERE id=$1::uuid`, institutionID).Scan(&enforced); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load roster status")
			return
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE roster_role='student' AND active), COUNT(*) FILTER (WHERE roster_role='faculty' AND active) FROM public.institution_roster WHERE institution_id=$1::uuid`, institutionID).Scan(&students, &faculty); err != nil {
			writeError(w, http.StatusInternalServerError, "could not load roster")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"roster_enforced": enforced, "students": students, "faculty": faculty, "max_rows": rosterMaxRows})
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
		defer r.Body.Close()
		var in struct {
			Rows []rosterInput `json:"rows"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil || len(in.Rows) == 0 || len(in.Rows) > rosterMaxRows {
			writeError(w, http.StatusBadRequest, "rows must contain between 1 and 10000 valid records")
			return
		}
		type prepared struct{ emailHash, externalID, role string }
		rows := make([]prepared, 0, len(in.Rows))
		seenEmail, seenID := map[string]bool{}, map[string]bool{}
		for i, row := range in.Rows {
			role := strings.ToLower(strings.TrimSpace(row.Role))
			if role != "student" && role != "faculty" {
				writeError(w, http.StatusBadRequest, "row role must be student or faculty (row "+itoa(i+2)+")")
				return
			}
			email := ""
			if strings.TrimSpace(row.Email) != "" {
				var valid bool
				email, valid = parseEmail(row.Email)
				if !valid {
					writeError(w, http.StatusBadRequest, "invalid email in row "+itoa(i+2))
					return
				}
			}
			externalID := strings.TrimSpace(row.ExternalID)
			if len(externalID) > 128 || !utf8.ValidString(externalID) {
				writeError(w, http.StatusBadRequest, "invalid external_id in row "+itoa(i+2))
				return
			}
			if email == "" && externalID == "" {
				writeError(w, http.StatusBadRequest, "each row needs email or external_id (row "+itoa(i+2)+")")
				return
			}
			hash := ""
			if email != "" {
				var err error
				hash, err = rosterEmailHash(email)
				if err != nil {
					writeError(w, http.StatusServiceUnavailable, "roster hashing is not configured")
					return
				}
				if seenEmail[hash] {
					writeError(w, http.StatusBadRequest, "duplicate email in CSV")
					return
				}
				seenEmail[hash] = true
			}
			if externalID != "" {
				if seenID[externalID] {
					writeError(w, http.StatusBadRequest, "duplicate external_id in CSV")
					return
				}
				seenID[externalID] = true
			}
			rows = append(rows, prepared{hash, externalID, role})
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not start roster import")
			return
		}
		defer func() { _ = tx.Rollback() }()
		// Full sync: old entries are disabled first, then the uploaded set is upserted.
		if _, err = tx.ExecContext(ctx, `UPDATE public.institution_roster SET active=FALSE, updated_at=NOW() WHERE institution_id=$1::uuid`, institutionID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not prepare roster import")
			return
		}
		for _, row := range rows {
			// Upsert by email and external ID separately so either key may be omitted.
			var q string
			var args []any
			switch {
			case row.emailHash != "" && row.externalID != "":
				q = `INSERT INTO public.institution_roster(institution_id,email_hash,external_id,roster_role,active) VALUES($1::uuid,$2,$3,$4,TRUE) ON CONFLICT (institution_id,email_hash) DO UPDATE SET external_id=EXCLUDED.external_id,roster_role=EXCLUDED.roster_role,active=TRUE,updated_at=NOW()`
				args = []any{institutionID, row.emailHash, row.externalID, row.role}
			case row.emailHash != "":
				q = `INSERT INTO public.institution_roster(institution_id,email_hash,roster_role,active) VALUES($1::uuid,$2,$3,TRUE) ON CONFLICT (institution_id,email_hash) DO UPDATE SET roster_role=EXCLUDED.roster_role,active=TRUE,updated_at=NOW()`
				args = []any{institutionID, row.emailHash, row.role}
			default:
				q = `INSERT INTO public.institution_roster(institution_id,external_id,roster_role,active) VALUES($1::uuid,$2,$3,TRUE) ON CONFLICT (institution_id,external_id) DO UPDATE SET roster_role=EXCLUDED.roster_role,active=TRUE,updated_at=NOW()`
				args = []any{institutionID, row.externalID, row.role}
			}
			if _, err = tx.ExecContext(ctx, q, args...); err != nil {
				log.Printf("institution roster: import row failed: %v", err)
				writeError(w, http.StatusConflict, "roster contains conflicting email or external ID values; no changes were saved")
				return
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE public.institutions SET roster_enforced=TRUE, updated_at=NOW() WHERE id=$1::uuid`, institutionID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not enable roster enforcement")
			return
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO public.institution_audit(institution_id,actor_user_id,action,target_hash) VALUES($1::uuid,$2::uuid,'roster.import', $3)`, institutionID, actorID, "rows:"+itoa(len(rows))); err != nil {
			writeError(w, http.StatusInternalServerError, "could not audit roster import")
			return
		}
		if err = tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save roster")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": "Roster imported. Access is now restricted to listed identities.", "imported": len(rows)})
	default:
		methodNotAllowed(w, "GET, POST, OPTIONS")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
