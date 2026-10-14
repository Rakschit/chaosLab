package handlers

import (
	"context"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"auth-api/pkg/session"
)

const tooManyMsg = "too many attempts, please try again later"

// clientIP returns the caller's address. On Vercel the X-Vercel-Forwarded-For
// header is set by the platform and cannot be spoofed by the client.
func clientIP(r *http.Request) string {
	first := func(v string) string {
		if i := strings.Index(v, ","); i >= 0 {
			v = v[:i]
		}
		return strings.TrimSpace(v)
	}
	for _, h := range []string{"X-Vercel-Forwarded-For", "X-Real-Ip", "X-Forwarded-For"} {
		if v := first(r.Header.Get(h)); v != "" {
			return v
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// limiter wraps Redis-backed counters. If Redis is unreachable it fails open
// (and logs), so a limiter outage never blocks sign-ups or logins by itself.
type limiter struct{ rds *session.Redis }

func newLimiter() *limiter {
	rds, err := session.NewRedis()
	if err != nil {
		log.Printf("rate limiting disabled: %v", err)
		return &limiter{}
	}
	return &limiter{rds: rds}
}

// hit counts one event for key and, when it is over limit, writes a 429 and
// returns true. Callers must stop handling the request when it returns true.
func (l *limiter) hit(ctx context.Context, w http.ResponseWriter, key string, limit int64, window time.Duration) bool {
	if l.rds == nil {
		return false
	}
	n, err := l.rds.Hit(ctx, key, window)
	if err != nil {
		log.Printf("rate limit hit %q: %v", key, err)
		return false
	}
	if n <= limit {
		return false
	}
	l.reject(ctx, w, key, window)
	return true
}

// blocked reports (and answers with 429) when key has already reached limit,
// without counting this request.
func (l *limiter) blocked(ctx context.Context, w http.ResponseWriter, key string, limit int64, window time.Duration) bool {
	if l.rds == nil {
		return false
	}
	n, err := l.rds.Count(ctx, key)
	if err != nil {
		log.Printf("rate limit count %q: %v", key, err)
		return false
	}
	if n < limit {
		return false
	}
	l.reject(ctx, w, key, window)
	return true
}

// fail records one failed attempt against key.
func (l *limiter) fail(ctx context.Context, key string, window time.Duration) {
	if l.rds == nil {
		return
	}
	if _, err := l.rds.Hit(ctx, key, window); err != nil {
		log.Printf("rate limit fail %q: %v", key, err)
	}
}

// clear forgets key, for example after a successful login.
func (l *limiter) clear(ctx context.Context, key string) {
	if l.rds == nil {
		return
	}
	if err := l.rds.Reset(ctx, key); err != nil {
		log.Printf("rate limit clear %q: %v", key, err)
	}
}

func (l *limiter) reject(ctx context.Context, w http.ResponseWriter, key string, window time.Duration) {
	wait := l.rds.RetryAfter(ctx, key)
	if wait <= 0 {
		wait = window
	}
	secs := int(wait / time.Second)
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, http.StatusTooManyRequests, tooManyMsg)
}
