package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"core-api/internal/api/middleware"
	"core-api/internal/api/requests"
	"core-api/internal/api/responses"
	"core-api/internal/store"
)

// maxSavedField caps each saved answer so a huge string cannot bloat the table.
const maxSavedField = 200

func clip(s string) string {
	r := []rune(s)
	if len(r) > maxSavedField {
		return string(r[:maxSavedField])
	}
	return s
}

// recordStart notes when this user created the simulation, so the submit can
// measure the time taken. Best effort: the game still works if this fails.
func recordStart(r *http.Request, seed, environment, difficulty, classroomID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	db, err := store.Open(ctx)
	if err != nil {
		log.Printf("history: database unavailable: %v", err)
		return
	}
	userID := middleware.UserID(r.Context())
	if err := store.RecordStart(ctx, db, userID, seed, environment, difficulty, classroomID); err != nil {
		log.Printf("history: record start: %v", err)
	}
}

// saveSubmission stores a graded submission in the user's history. Best
// effort for normal games, so callers there ignore the returned error and the
// player still gets their grade. Classroom games check it.
func saveSubmission(r *http.Request, seed, classroomID string, req requests.SubmitSingleplayerRequest, result responses.SubmissionResponse) error {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	db, err := store.Open(ctx)
	if err != nil {
		log.Printf("history: database unavailable: %v", err)
		return err
	}

	saved := make([]store.SavedFinding, 0, len(req.Findings))
	for i, f := range req.Findings {
		sf := store.SavedFinding{
			SourceIP:    clip(f.SourceIP),
			Endpoint:    clip(f.Endpoint),
			AnomalyType: clip(f.AnomalyType),
		}
		if i < len(result.Findings) {
			fr := result.Findings[i]
			sf.Correct = fr.Correct
			sf.SourceIPOK = fr.SourceIP
			sf.EndpointOK = fr.Endpoint
			sf.AnomalyTypeOK = fr.AnomalyType
		}
		saved = append(saved, sf)
	}

	_, err = store.SaveResult(ctx, db, store.Result{
		UserID:      middleware.UserID(r.Context()),
		Seed:        seed,
		Environment: req.Environment,
		Difficulty:  req.Difficulty,
		Score:       result.Score,
		Matched:     result.Matched,
		Total:       result.Total,
		Correct:     result.Correct,
		Findings:    saved,
		ClassroomID: classroomID,
	})
	if err != nil {
		log.Printf("history: save result: %v", err)
	}
	return err
}
