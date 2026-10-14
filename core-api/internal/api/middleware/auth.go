// Package middleware holds HTTP middleware shared by the core-api routes.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"core-api/internal/api/responses"
)

type ctxKey struct{}
type planKey struct{}
type institutionKey struct{}
type profileKey struct{}

// Profile is the logged-in person's name and email, as reported by auth-api.
// Classrooms copy it when a student joins, so faculty can read their results.
type Profile struct {
	DisplayName string
	Email       string
}

// Institution is the institution a user belongs to, as reported by auth-api.
// It is only present while the institution plan is valid (active institution
// with an unexpired license).
type Institution struct {
	ID            string
	Name          string
	MemberRole    string // "student" or "faculty"
	MonthlyLimits map[string]int64
}

var authClient = &http.Client{Timeout: 5 * time.Second}

var (
	errUnauthorized = errors.New("unauthorized")
	errUnavailable  = errors.New("auth service unavailable")
)

// UserID returns the authenticated user's ID set by Auth ("" if none).
func UserID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Plan returns the authenticated user's plan: "institution", "pro" or "free".
// Anything else (including a missing value) counts as free, the safer default.
// "institution" is only returned when auth-api also sent the institution's
// limits; without them it falls back to free.
func Plan(ctx context.Context) string {
	switch p, _ := ctx.Value(planKey{}).(string); p {
	case "pro":
		return "pro"
	case "institution":
		if InstitutionOf(ctx) != nil {
			return "institution"
		}
	}
	return "free"
}

// ProfileOf returns the authenticated user's name and email.
func ProfileOf(ctx context.Context) Profile {
	p, _ := ctx.Value(profileKey{}).(Profile)
	return p
}

// InstitutionOf returns the user's institution, or nil if they have none.
func InstitutionOf(ctx context.Context) *Institution {
	inst, _ := ctx.Value(institutionKey{}).(*Institution)
	return inst
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(responses.ErrorResponse{Error: message})
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(h) < 8 || !strings.EqualFold(h[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

// Auth rejects requests without a valid login session. The token is checked
// by asking auth-api (AUTH_API_URL) who it belongs to, so core-api never
// needs the session store or database credentials. Fails closed: if auth-api
// cannot be reached the request is refused.
func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		userID, plan, inst, profile, err := lookupUser(r.Context(), token)
		switch {
		case errors.Is(err, errUnauthorized):
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		case err != nil:
			log.Printf("auth: %v", err)
			writeError(w, http.StatusServiceUnavailable, "service temporarily unavailable")
			return
		}

		ctx := context.WithValue(r.Context(), ctxKey{}, userID)
		ctx = context.WithValue(ctx, planKey{}, plan)
		ctx = context.WithValue(ctx, profileKey{}, profile)
		if inst != nil {
			ctx = context.WithValue(ctx, institutionKey{}, inst)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func lookupUser(ctx context.Context, token string) (userID, plan string, inst *Institution, profile Profile, err error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("AUTH_API_URL")), "/")
	if base == "" {
		return "", "", nil, Profile{}, errors.New("AUTH_API_URL is not set")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/auth/me", nil)
	if err != nil {
		return "", "", nil, Profile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := authClient.Do(req)
	if err != nil {
		return "", "", nil, Profile{}, errUnavailable
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return "", "", nil, Profile{}, errUnauthorized
	case resp.StatusCode != http.StatusOK:
		return "", "", nil, Profile{}, errUnavailable
	}

	var body struct {
		User struct {
			ID          string `json:"id"`
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
			Plan        string `json:"plan"`
			Institution *struct {
				ID            string           `json:"id"`
				Name          string           `json:"name"`
				MemberRole    string           `json:"member_role"`
				MonthlyLimits map[string]int64 `json:"monthly_limits"`
			} `json:"institution"`
		} `json:"user"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil || body.User.ID == "" {
		return "", "", nil, Profile{}, errUnavailable
	}
	if in := body.User.Institution; in != nil && in.ID != "" {
		inst = &Institution{ID: in.ID, Name: in.Name, MemberRole: in.MemberRole, MonthlyLimits: in.MonthlyLimits}
	}
	return body.User.ID, body.User.Plan, inst, Profile{DisplayName: body.User.DisplayName, Email: body.User.Email}, nil
}
