package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"core-api/internal/api/middleware"
)

// fakeRedis answers the few Upstash REST commands the limiter uses.
func fakeRedis(t *testing.T) {
	t.Helper()
	var mu sync.Mutex
	counters := map[string]int64{}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var args []any
		_ = json.NewDecoder(r.Body).Decode(&args)
		cmd, key := args[0].(string), args[1].(string)

		mu.Lock()
		defer mu.Unlock()
		var res any
		switch cmd {
		case "INCR":
			counters[key]++
			res = counters[key]
		case "DECR":
			counters[key]--
			res = counters[key]
		case "SET":
			counters[key] = 0
			res = "OK"
		case "DEL":
			delete(counters, key)
			res = 1
		default: // EXPIRE, TTL
			res = 1
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": res})
	}))
	t.Cleanup(srv.Close)

	old := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = old })

	t.Setenv("UPSTASH_REDIS_REST_URL", srv.URL)
	t.Setenv("UPSTASH_REDIS_REST_TOKEN", "test")
}

// statuses calls chargeRoomCreate n times as a user on the given plan.
func statuses(t *testing.T, plan string, lim roomPlan, n int) []int {
	t.Helper()
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"user":{"id":"user-%s","plan":"%s","display_name":"d","email":"e"}}`, plan, plan)
	}))
	t.Cleanup(auth.Close)
	t.Setenv("AUTH_API_URL", auth.URL)

	h := middleware.Auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := chargeRoomCreate(w, r, middleware.Plan(r.Context()), lim); ok {
			w.WriteHeader(http.StatusCreated)
		}
	}))

	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set("Authorization", "Bearer token")
		h.ServeHTTP(rec, req)
		out = append(out, rec.Code)
	}
	return out
}

func count(codes []int, want int) int {
	n := 0
	for _, c := range codes {
		if c == want {
			n++
		}
	}
	return n
}

// The hourly limit stops free users after 2 rooms and pro users after 10, and
// refused attempts do not use anything up.
func TestRoomCreateHourlyLimit(t *testing.T) {
	fakeRedis(t)
	for _, plan := range []string{"free", "pro"} {
		lim := roomPlanFor(plan)
		codes := statuses(t, plan, lim, int(lim.PerHour)+4)
		if got := count(codes, http.StatusCreated); int64(got) != lim.PerHour {
			t.Fatalf("%s: %d rooms allowed in an hour, want %d (%v)", plan, got, lim.PerHour, codes)
		}
		if got := count(codes, http.StatusTooManyRequests); got != 4 {
			t.Fatalf("%s: %d refusals, want 4 (%v)", plan, got, codes)
		}
	}
}

// With the hourly cap out of the way, the monthly cap applies.
func TestRoomCreateMonthlyLimit(t *testing.T) {
	fakeRedis(t)
	lim := roomPlan{PerHour: 100, PerMonth: 5, MaxActive: 1, MaxPlayers: 4}
	codes := statuses(t, "free", lim, 8)
	if got := count(codes, http.StatusCreated); got != 5 {
		t.Fatalf("%d rooms allowed in a month, want 5 (%v)", got, codes)
	}
}

func TestRoomPlanFor(t *testing.T) {
	if roomPlanFor("institution") != roomPlans["pro"] {
		t.Error("institution should use the pro limits")
	}
	if roomPlanFor("") != roomPlans["free"] || roomPlanFor("nonsense") != roomPlans["free"] {
		t.Error("unknown plans should use the free limits")
	}
	if roomPlans["pro"].PerMonth <= roomPlans["free"].PerMonth {
		t.Error("pro should allow more rooms than free")
	}
}
