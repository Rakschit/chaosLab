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

// roomPlan is what one plan may do with multiplayer rooms.
//
// Joining a room is always free. Only CREATING a room is limited, in four ways:
//   - PerHour      how many rooms you can create in one clock hour (UTC)
//   - PerMonth     how many rooms you can create in one calendar month (UTC)
//   - MaxActive    how many open rooms (waiting or running) you can host at once
//   - MaxPlayers   the biggest room you can create
//
// To change a limit, edit the numbers below. Institution accounts use the Pro
// numbers.
type roomPlan struct {
	PerHour    int64 `json:"per_hour"`
	PerMonth   int64 `json:"per_month"`
	MaxActive  int   `json:"max_active"`
	MaxPlayers int   `json:"max_players"`
}

var roomPlans = map[string]roomPlan{
	"free": {PerHour: 2, PerMonth: 5, MaxActive: 1, MaxPlayers: 4},
	"pro":  {PerHour: 10, PerMonth: 40, MaxActive: 3, MaxPlayers: 8},
}

// roomPlanFor returns the room limits for a plan name from middleware.Plan.
func roomPlanFor(plan string) roomPlan {
	if plan == "institution" {
		plan = "pro"
	}
	if p, ok := roomPlans[plan]; ok {
		return p
	}
	return roomPlans["free"]
}

const hourWindow = 2 * time.Hour // the hour key is per clock hour; it only needs to outlive it

func hourKey(userID string, at time.Time) string {
	return "mp-create-hour:" + userID + ":" + at.UTC().Format("2006-01-02T15")
}

func roomMonthKey(userID string, at time.Time) string {
	return "mp-create-month:" + userID + ":" + at.UTC().Format("2006-01")
}

func nextHourStart(at time.Time) time.Time {
	return at.UTC().Truncate(time.Hour).Add(time.Hour)
}

// chargeRoomCreate counts one room creation against the hourly and monthly
// limits. It returns (refund, true) when allowed; call refund() if creating the
// room then fails, so a failed attempt does not use up a room. When a limit is
// reached it writes a 429 and returns false (the counters are put back, so
// refused attempts never count). Like the other limits, it logs and allows the
// request if Redis is unavailable.
func chargeRoomCreate(w http.ResponseWriter, r *http.Request, plan string, lim roomPlan) (func(), bool) {
	noop := func() {}

	limiter, err := ratelimit.New()
	if err != nil {
		log.Printf("room create limit: disabled: %v", err)
		return noop, true
	}

	userID := middleware.UserID(r.Context())
	now := time.Now().UTC()
	hKey, mKey := hourKey(userID, now), roomMonthKey(userID, now)

	refund := func(keys ...string) {
		for _, k := range keys {
			if err := limiter.Refund(r.Context(), k); err != nil {
				log.Printf("room create limit: refund: %v", err)
			}
		}
	}

	n, err := limiter.Hit(r.Context(), hKey, hourWindow)
	if err != nil {
		log.Printf("room create limit: %v", err)
		return noop, true
	}
	if n > lim.PerHour {
		refund(hKey)
		w.Header().Set("Retry-After", strconv.Itoa(int(nextHourStart(now).Sub(now).Seconds())))
		msg := fmt.Sprintf("Room limit reached: %d new rooms per hour on the %s plan. Try again in a bit.", lim.PerHour, plan)
		if plan == "free" {
			msg += fmt.Sprintf(" Pro allows %d.", roomPlans["pro"].PerHour)
		}
		writeError(w, http.StatusTooManyRequests, msg)
		return nil, false
	}

	n, err = limiter.Hit(r.Context(), mKey, 40*24*time.Hour)
	if err != nil {
		log.Printf("room create limit: %v", err)
		return func() { refund(hKey) }, true
	}
	if n > lim.PerMonth {
		refund(hKey, mKey)
		w.Header().Set("Retry-After", strconv.Itoa(int(nextMonthStart(now).Sub(now).Seconds())))
		msg := fmt.Sprintf("Monthly room limit reached: %d rooms on the %s plan. It resets on the 1st at 00:00 UTC.", lim.PerMonth, plan)
		if plan == "free" {
			msg += fmt.Sprintf(" Pro allows %d.", roomPlans["pro"].PerMonth)
		}
		writeError(w, http.StatusTooManyRequests, msg)
		return nil, false
	}

	return func() { refund(hKey, mKey) }, true
}
