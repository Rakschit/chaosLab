package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

const maxFeedbackComment = 1000

// SubmitFeedback stores feedback from a completed mission.
func SubmitFeedback(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) { return }
	if r.Method != http.MethodPost { methodNotAllowed(w, "POST, OPTIONS"); return }
	w.Header().Set("Cache-Control", "no-store")

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	userID, ok := requireUser(ctx, w, r)
	if !ok { return }

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	var input struct {
		Rating int `json:"rating"`
		Difficulty string `json:"difficulty"`
		Seed string `json:"seed"`
		Score *int `json:"score"`
		Comment string `json:"comment"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil { writeError(w, http.StatusBadRequest, "invalid request body"); return }
	if input.Rating < 1 || input.Rating > 5 { writeError(w, http.StatusBadRequest, "rating must be between 1 and 5"); return }
	if input.Score != nil && (*input.Score < 0 || *input.Score > 100) { writeError(w, http.StatusBadRequest, "score must be between 0 and 100"); return }
	comment := strings.TrimSpace(input.Comment)
	if len([]rune(comment)) > maxFeedbackComment { writeError(w, http.StatusBadRequest, "comment must be at most 1000 characters"); return }
	difficulty := strings.TrimSpace(input.Difficulty)
	seed := strings.TrimSpace(input.Seed)
	if len([]rune(difficulty)) > 20 || len([]rune(seed)) > 64 { writeError(w, http.StatusBadRequest, "mission details are too long"); return }

	db, err := database.Open(ctx)
	if err != nil { log.Printf("feedback: database unavailable: %v", err); writeError(w, http.StatusServiceUnavailable, "database unavailable"); return }
	_, err = db.ExecContext(ctx, `
		INSERT INTO public.mission_feedback (user_id, rating, difficulty, seed, score, comment)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)
	`, userID, input.Rating, difficulty, seed, input.Score, comment)
	if err != nil { log.Printf("feedback: insert: %v", err); writeError(w, http.StatusInternalServerError, "could not save feedback"); return }
	writeJSON(w, http.StatusCreated, map[string]string{"message": "Feedback saved for admin review."})
}

// AdminFeedback lists recent mission feedback for admins.
func AdminFeedback(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) { return }
	if r.Method != http.MethodGet { methodNotAllowed(w, "GET, OPTIONS"); return }
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	db, _, ok := adminUser(ctx, w, r)
	if !ok { return }

	rows, err := db.QueryContext(ctx, `
		SELECT f.id::text, f.rating, f.difficulty, f.seed, f.score, f.comment,
		       f.created_at, u.email, u.display_name
		FROM public.mission_feedback f
		JOIN public.users u ON u.id = f.user_id
		ORDER BY f.created_at DESC
		LIMIT 200
	`)
	if err != nil { log.Printf("admin feedback: query: %v", err); writeError(w, http.StatusInternalServerError, "could not load feedback"); return }
	defer rows.Close()
	type item struct { ID string `json:"id"`; Rating int `json:"rating"`; Difficulty string `json:"difficulty"`; Seed string `json:"seed"`; Score *int `json:"score"`; Comment string `json:"comment"`; CreatedAt time.Time `json:"created_at"`; Email string `json:"email"`; DisplayName string `json:"display_name"` }
	list := []item{}
	for rows.Next() {
		var it item
		var score sql.NullInt64
		if err := rows.Scan(&it.ID, &it.Rating, &it.Difficulty, &it.Seed, &score, &it.Comment, &it.CreatedAt, &it.Email, &it.DisplayName); err != nil { log.Printf("admin feedback: scan: %v", err); writeError(w, http.StatusInternalServerError, "could not load feedback"); return }
		if score.Valid { n := int(score.Int64); it.Score = &n }
		list = append(list, it)
	}
	if err := rows.Err(); err != nil { writeError(w, http.StatusInternalServerError, "could not load feedback"); return }
	writeJSON(w, http.StatusOK, map[string]any{"feedback": list})
}

