package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	maxPlanText       = 500
	roleAdmin         = "admin"
	planPro           = "pro"
	pgUniqueViolation = "23505"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// cleanText trims free text and enforces the length limit and a ban on
// control characters (other than line breaks and tabs).
func cleanText(raw string) (string, bool) {
	text := strings.TrimSpace(raw)
	if utf8.RuneCountInString(text) > maxPlanText {
		return "", false
	}
	for _, c := range text {
		if unicode.IsControl(c) && c != '\n' && c != '\r' && c != '\t' {
			return "", false
		}
	}
	return text, true
}

type planRequestJSON struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	Message      string     `json:"message"`
	DecisionNote string     `json:"decision_note"`
	CreatedAt    time.Time  `json:"created_at"`
	DecidedAt    *time.Time `json:"decided_at"`
}

// adminUser authenticates the caller and checks, in the database on every
// call, that they are an active admin. On failure it has already written the
// response and ok is false.
func adminUser(ctx context.Context, w http.ResponseWriter, r *http.Request) (*sql.DB, string, bool) {
	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return nil, "", false
	}
	if newLimiter().hit(ctx, w, "admin:user:"+userID, 300, time.Hour) {
		return nil, "", false
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("admin: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return nil, "", false
	}

	var role string
	err = db.QueryRowContext(ctx, `
		SELECT role
		FROM public.users
		WHERE id = $1::uuid AND status = 'active' AND email_verified_at IS NOT NULL
	`, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return nil, "", false
	}
	if err != nil {
		log.Printf("admin: look up role: %v", err)
		writeError(w, http.StatusInternalServerError, "could not check permissions")
		return nil, "", false
	}
	if role != roleAdmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return nil, "", false
	}
	return db, userID, true
}

// PlanRequest handles the logged-in user's own Pro request:
// GET returns the latest one, POST creates a new one.
func PlanRequest(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	switch r.Method {
	case http.MethodGet:
		getPlanRequest(w, r)
	case http.MethodPost:
		createPlanRequest(w, r)
	default:
		methodNotAllowed(w, "GET, POST, OPTIONS")
	}
}

func getPlanRequest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("plan-request: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	var pr planRequestJSON
	var decidedAt sql.NullTime
	err = db.QueryRowContext(ctx, `
		SELECT id::text, status, message, decision_note, created_at, decided_at
		FROM public.plan_requests
		WHERE user_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT 1
	`, userID).Scan(&pr.ID, &pr.Status, &pr.Message, &pr.DecisionNote, &pr.CreatedAt, &decidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"request": nil})
		return
	}
	if err != nil {
		log.Printf("plan-request: load: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load your request")
		return
	}
	if decidedAt.Valid {
		pr.DecidedAt = &decidedAt.Time
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": pr})
}

func createPlanRequest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	userID, ok := requireUser(ctx, w, r)
	if !ok {
		return
	}
	if newLimiter().hit(ctx, w, "planreq:user:"+userID, 5, time.Hour) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var input struct {
		Message string `json:"message"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	message, ok := cleanText(input.Message)
	if !ok {
		writeError(w, http.StatusBadRequest, "message must be at most 500 characters, with no control characters")
		return
	}

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("plan-request: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	var email, displayName, plan string
	err = db.QueryRowContext(ctx, `
		SELECT email, display_name, plan
		FROM public.users
		WHERE id = $1::uuid AND status = 'active' AND email_verified_at IS NOT NULL
	`, userID).Scan(&email, &displayName, &plan)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}
	if err != nil {
		log.Printf("plan-request: look up user: %v", err)
		writeError(w, http.StatusInternalServerError, "could not send request")
		return
	}
	if plan == planPro {
		writeError(w, http.StatusConflict, "you already have the Pro plan")
		return
	}

	var pr planRequestJSON
	err = db.QueryRowContext(ctx, `
		INSERT INTO public.plan_requests (user_id, message)
		VALUES ($1::uuid, $2)
		RETURNING id::text, status, message, decision_note, created_at
	`, userID, message).Scan(&pr.ID, &pr.Status, &pr.Message, &pr.DecisionNote, &pr.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			writeError(w, http.StatusConflict, "you already have a pending request")
			return
		}
		log.Printf("plan-request: insert: %v", err)
		writeError(w, http.StatusInternalServerError, "could not send request")
		return
	}

	// Best effort: the request is saved even if the email cannot be sent.
	notifyAdminOfRequest(ctx, email, displayName, message)

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": "Request sent. You will get an email when it is reviewed.",
		"request": pr,
	})
}

func notifyAdminOfRequest(ctx context.Context, userEmail, displayName, message string) {
	adminEmail := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))
	if adminEmail == "" {
		log.Printf("plan-request: ADMIN_EMAIL is not set, admin was not notified")
		return
	}
	svc, err := services.NewEmailService()
	if err != nil {
		log.Printf("plan-request: email service unavailable: %v", err)
		return
	}
	if err := svc.SendPlanRequestAdminEmail(ctx, adminEmail, userEmail, displayName, message); err != nil {
		log.Printf("plan-request: admin email failed: %v", err)
	}
}

// AdminPlanRequests lists pending Pro requests (admin only).
func AdminPlanRequests(w http.ResponseWriter, r *http.Request) {
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
		SELECT pr.id::text, pr.message, pr.created_at, u.email, u.display_name
		FROM public.plan_requests pr
		JOIN public.users u ON u.id = pr.user_id
		WHERE pr.status = 'pending'
		ORDER BY pr.created_at ASC
		LIMIT 100
	`)
	if err != nil {
		log.Printf("admin plan requests: query: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load requests")
		return
	}
	defer rows.Close()

	type item struct {
		ID          string    `json:"id"`
		Message     string    `json:"message"`
		CreatedAt   time.Time `json:"created_at"`
		Email       string    `json:"email"`
		DisplayName string    `json:"display_name"`
	}
	list := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Message, &it.CreatedAt, &it.Email, &it.DisplayName); err != nil {
			log.Printf("admin plan requests: scan: %v", err)
			writeError(w, http.StatusInternalServerError, "could not load requests")
			return
		}
		list = append(list, it)
	}
	if err := rows.Err(); err != nil {
		log.Printf("admin plan requests: rows: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load requests")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"requests": list})
}

// AdminPlanDecision approves or rejects one pending request (admin only).
// Approving also sets the user's plan to pro, in the same transaction.
func AdminPlanDecision(w http.ResponseWriter, r *http.Request) {
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

	db, adminID, ok := adminUser(ctx, w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var input struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
		Note     string `json:"note"`
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
	var status string
	switch input.Decision {
	case "approve":
		status = "approved"
	case "reject":
		status = "rejected"
	default:
		writeError(w, http.StatusBadRequest, "decision must be approve or reject")
		return
	}
	note, ok := cleanText(input.Note)
	if !ok {
		writeError(w, http.StatusBadRequest, "note must be at most 500 characters, with no control characters")
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("admin decision: begin: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}
	defer tx.Rollback()

	var targetID string
	err = tx.QueryRowContext(ctx, `
		UPDATE public.plan_requests
		SET status = $2, decision_note = $3, decided_by = $4::uuid, decided_at = NOW()
		WHERE id = $1::uuid AND status = 'pending'
		RETURNING user_id::text
	`, input.ID, status, note, adminID).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusConflict, "request not found or already decided")
		return
	}
	if err != nil {
		log.Printf("admin decision: update request: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}

	if status == "approved" {
		res, err := tx.ExecContext(ctx, `
			UPDATE public.users SET plan = 'pro', updated_at = NOW()
			WHERE id = $1::uuid
		`, targetID)
		if err != nil {
			log.Printf("admin decision: set plan: %v", err)
			writeError(w, http.StatusInternalServerError, "could not save decision")
			return
		}
		if n, _ := res.RowsAffected(); n != 1 {
			log.Printf("admin decision: set plan affected %d rows", n)
			writeError(w, http.StatusInternalServerError, "could not save decision")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("admin decision: commit: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save decision")
		return
	}

	// Best effort: the decision is already saved.
	notifyUserOfDecision(ctx, db, targetID, status == "approved", note)

	msg := "Request rejected."
	if status == "approved" {
		msg = "Request approved. The user is now on Pro."
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": msg})
}

func notifyUserOfDecision(ctx context.Context, db *sql.DB, userID string, approved bool, note string) {
	var email, displayName string
	err := db.QueryRowContext(ctx, `
		SELECT email, display_name FROM public.users WHERE id = $1::uuid
	`, userID).Scan(&email, &displayName)
	if err != nil {
		log.Printf("admin decision: load user for email: %v", err)
		return
	}
	svc, err := services.NewEmailService()
	if err != nil {
		log.Printf("admin decision: email service unavailable: %v", err)
		return
	}
	if err := svc.SendPlanDecisionEmail(ctx, email, displayName, approved, note); err != nil {
		log.Printf("admin decision: user email failed: %v", err)
	}
}
