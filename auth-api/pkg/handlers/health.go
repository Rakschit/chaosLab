package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"auth-api/internal/database"
)

// Health reports whether the API can reach the database.
func Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	db, err := database.Open(ctx)
	if err != nil {
		log.Printf("health: database open error: %v", err)
		writeError(w, http.StatusInternalServerError, "database unavailable")
		return
	}

	var result int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		log.Printf("health: database ping error: %v", err)
		writeError(w, http.StatusServiceUnavailable, "database connection failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"service":  "auth-api",
		"status":   "running",
		"database": "connected",
	})
}
