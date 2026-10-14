package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"auth-api/internal/auth"
	"auth-api/pkg/session"
)

// Logout revokes the caller's session in Redis, so the token stops working
// immediately everywhere. It is idempotent: an unknown or already-expired
// token still gets a 200.
func Logout(w http.ResponseWriter, r *http.Request) {
	if applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST, OPTIONS")
		return
	}

	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	redis, err := session.NewRedis()
	if err != nil {
		log.Printf("logout: redis unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := redis.Delete(ctx, auth.HashSessionToken(token)); err != nil {
		log.Printf("logout: delete session: %v", err)
		writeError(w, http.StatusServiceUnavailable, "could not log out")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}
