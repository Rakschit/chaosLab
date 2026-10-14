package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"core-api/internal/api/middleware"
	"core-api/internal/store"
)

func intParam(r *http.Request, name string, def, min, max int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// SingleplayerHistory lists the logged-in user's own submissions, newest first.
// Query: limit (1-100, default 50) and offset (default 0).
func SingleplayerHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	limit := intParam(r, "limit", 50, 1, 100)
	offset := intParam(r, "offset", 0, 0, 10000)

	db, err := store.Open(ctx)
	if err != nil {
		log.Printf("history: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "history is temporarily unavailable")
		return
	}

	items, more, err := store.ListResults(ctx, db, middleware.UserID(r.Context()), limit, offset)
	if err != nil {
		log.Printf("history: list: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load history")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"results":  items,
		"has_more": more,
	})
}