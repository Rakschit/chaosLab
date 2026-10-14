package middleware

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"core-api/internal/ratelimit"
)

// RateLimit allows each logged-in user at most limit requests per window for
// the route named name. It must run after Auth, because it keys on the user.
// If Redis is unavailable it logs and lets the request through, so a Redis
// outage does not take the game down.
func RateLimit(name string, limit int64, window time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limiter, err := ratelimit.New()
		if err != nil {
			log.Printf("ratelimit %s: disabled: %v", name, err)
			next.ServeHTTP(w, r)
			return
		}

		key := name + ":" + UserID(r.Context())
		n, err := limiter.Hit(r.Context(), key, window)
		if err != nil {
			log.Printf("ratelimit %s: %v", name, err)
			next.ServeHTTP(w, r)
			return
		}

		if n > limit {
			secs := int(limiter.RetryAfter(r.Context(), key) / time.Second)
			if secs < 1 {
				secs = int(window / time.Second)
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeError(w, http.StatusTooManyRequests, "too many requests, please slow down")
			return
		}

		next.ServeHTTP(w, r)
	})
}
