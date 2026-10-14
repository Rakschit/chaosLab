package handlers

import (
	"log"
	"net/http"
	"time"

	"core-api/internal/api/middleware"
	"core-api/internal/ratelimit"
)

type limitStatus struct {
	Limit     int64 `json:"limit"`
	Used      int64 `json:"used"`
	Remaining int64 `json:"remaining"`
}

// SingleplayerLimits reports how many singleplayer games the logged-in user
// has left this month for each difficulty. It only reads the counters; it never
// uses up a game. If the counters cannot be read, the limits are still
// returned with usage_known set to false.
func SingleplayerLimits(w http.ResponseWriter, r *http.Request) {
	plan := middleware.Plan(r.Context())
	userID := middleware.UserID(r.Context())

	now := time.Now().UTC()
	resetsAt := nextMonthStart(now)

	usageKnown := true
	limiter, err := ratelimit.New()
	if err != nil {
		log.Printf("limits: counters unavailable: %v", err)
		usageKnown = false
		limiter = nil
	}

	levels := []string{"easy", "medium", "hard"}
	limits := make(map[string]limitStatus, len(levels))
	for _, level := range levels {
		limit, _ := limitFor(r, plan, level)
		status := limitStatus{Limit: limit, Remaining: limit}

		if limiter != nil {
			used, err := limiter.Count(r.Context(), monthKey(userID, level, now))
			if err != nil {
				log.Printf("limits: read %s: %v", level, err)
				usageKnown = false
			} else {
				// Requests refused at the limit also bump the counter; never show more than the limit.
				if used > limit {
					used = limit
				}
				status.Used = used
				status.Remaining = limit - used
			}
		}
		limits[level] = status
	}

	resp := map[string]any{
		"plan":        plan,
		"resets_at":   resetsAt.Format(time.RFC3339),
		"usage_known": usageKnown,
		"limits":      limits,
	}
	if plan == "free" {
		pro := make(map[string]int64, len(levels))
		for _, level := range levels {
			pro[level] = monthlyLimits["pro"][level]
		}
		resp["pro_limits"] = pro
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}
