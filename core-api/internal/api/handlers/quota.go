package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"core-api/internal/api/middleware"
	"core-api/internal/ratelimit"
)

// monthlyLimits is how many singleplayer games a user may start per month, by
// plan and difficulty. The month is a UTC calendar month: it resets at 00:00
// UTC on the 1st. To change a limit, edit the number here.
var monthlyLimits = map[string]map[string]int64{
	"free": {"easy": 10, "medium": 5, "hard": 3},
	"pro":  {"easy": 40, "medium": 30, "hard": 20},
}

// limitFor returns the monthly limit for this request's user and difficulty.
// Institution users get the limits their institution is licensed for; everyone
// else uses the fixed table above. ok is false when no limit applies.
func limitFor(r *http.Request, plan, difficulty string) (int64, bool) {
	if plan == "institution" {
		if inst := middleware.InstitutionOf(r.Context()); inst != nil {
			limit, ok := inst.MonthlyLimits[difficulty]
			return limit, ok
		}
	}
	limit, ok := monthlyLimits[plan][difficulty]
	return limit, ok
}

// monthKey names the counter for one user, difficulty and UTC month. The month
// is part of the key, so a new month starts a fresh counter. Used by both
// allowStart and SingleplayerLimits so they always read and write the same one.
func monthKey(userID, difficulty string, at time.Time) string {
	return "monthly:" + userID + ":" + difficulty + ":" + at.UTC().Format("2006-01")
}

// nextMonthStart is when the current UTC month ends (00:00 UTC on the 1st).
func nextMonthStart(at time.Time) time.Time {
	at = at.UTC()
	return time.Date(at.Year(), at.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

// allowStart counts one started game against the user's monthly limit for this
// difficulty. It returns false after writing a 429 when the limit is used up.
// The plan comes from auth-api on every request, so an approved Pro request
// takes effect immediately. If Redis is unavailable it logs and allows the
// game, like the other rate limits.
func allowStart(w http.ResponseWriter, r *http.Request, difficulty string) bool {
	plan := middleware.Plan(r.Context())
	limit, ok := limitFor(r, plan, difficulty)
	if !ok {
		return true
	}

	limiter, err := ratelimit.New()
	if err != nil {
		log.Printf("monthly limit: disabled: %v", err)
		return true
	}

	now := time.Now().UTC()
	key := monthKey(middleware.UserID(r.Context()), difficulty, now)

	// The key already contains the month, so it only needs to outlive it.
	n, err := limiter.Hit(r.Context(), key, 40*24*time.Hour)
	if err != nil {
		log.Printf("monthly limit: %v", err)
		return true
	}

	if n > limit {
		w.Header().Set("Retry-After", strconv.Itoa(int(nextMonthStart(now).Sub(now).Seconds())))

		msg := fmt.Sprintf("Monthly limit reached: %d %s games on the %s plan. It resets on the 1st at 00:00 UTC.", limit, difficulty, plan)
		if plan == "free" {
			msg += fmt.Sprintf(" Pro allows %d.", monthlyLimits["pro"][difficulty])
		}
		writeError(w, http.StatusTooManyRequests, msg)
		return false
	}
	return true
}
