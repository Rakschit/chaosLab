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

// saveRoomSubmission puts a graded multiplayer answer into the player's history
// (the host is a player too, so the same code covers both). Best effort: the
// scoreboard is already updated, so a failure here only costs a history row.
func saveRoomSubmission(r *http.Request, room store.Room, req requests.SubmitSingleplayerRequest, result responses.SubmissionResponse) {
	if room.StartedAt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	db, err := store.Open(ctx)
	if err != nil {
		log.Printf("history: database unavailable: %v", err)
		return
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

	_, err = store.SaveRoomResult(ctx, db, store.Result{
		UserID:      middleware.UserID(r.Context()),
		Seed:        room.Seed,
		Environment: room.Environment,
		Difficulty:  room.Difficulty,
		Score:       result.Score,
		Matched:     result.Matched,
		Total:       result.Total,
		Correct:     result.Correct,
		Findings:    saved,
	}, room.Code, *room.StartedAt)
	if err != nil {
		log.Printf("history: save room result: %v", err)
	}
}
