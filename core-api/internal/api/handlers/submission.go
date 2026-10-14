package handlers

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"core-api/internal/api/middleware"
	"core-api/internal/api/requests"
	"core-api/internal/ratelimit"
	"core-api/services"
)

const (
	// maxAttemptsPerSeed caps how often one user may submit answers for one
	// seed. Submissions return per-field feedback, so without a cap the answers
	// could be found by guessing.
	maxAttemptsPerSeed = 10
	attemptWindow      = time.Hour
)

// SubmitSingleplayer validates a player's findings. Method routing, CORS,
// login and the per-minute rate limit are handled in router.go.
func SubmitSingleplayer(w http.ResponseWriter, r *http.Request) {
	var req requests.SubmitSingleplayerRequest

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Only well-formed seeds get a counter, so junk cannot fill Redis with keys.
	seed := strings.ToLower(strings.TrimSpace(req.Seed))
	if b, err := hex.DecodeString(seed); err != nil || len(b) != 8 {
		writeError(w, http.StatusBadRequest, "invalid seed: must be 16 hex characters")
		return
	}

	if !allowAttempt(w, r, seed) {
		return
	}

	result, err := services.ValidateSingleplayerSubmission(req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	// Store the graded result in the user's history (best effort).
	saveSubmission(r, seed, "", req, result)

	writeJSON(w, http.StatusOK, result)
}

// allowAttempt counts one submission for this user and seed. It returns false
// after writing a 429 when the cap is exceeded. If Redis is unavailable it
// logs and allows the request.
func allowAttempt(w http.ResponseWriter, r *http.Request, seed string) bool {
	limiter, err := ratelimit.New()
	if err != nil {
		log.Printf("attempt cap: disabled: %v", err)
		return true
	}

	key := "attempt:" + middleware.UserID(r.Context()) + ":" + seed
	n, err := limiter.Hit(r.Context(), key, attemptWindow)
	if err != nil {
		log.Printf("attempt cap: %v", err)
		return true
	}

	if n > maxAttemptsPerSeed {
		secs := int(limiter.RetryAfter(r.Context(), key) / time.Second)
		if secs < 1 {
			secs = int(attemptWindow / time.Second)
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeError(w, http.StatusTooManyRequests, "too many attempts for this seed, try again later")
		return false
	}
	return true
}