package core_api

import (
	"net/http"
	"os"
	"strings"
	"time"

	"core-api/internal/api/handlers"
	"core-api/internal/api/middleware"
)

// NewRouter creates the application's HTTP router.
// Keep all public API endpoints in this file.
func NewRouter() http.Handler {
	mux := http.NewServeMux()

	// Singleplayer. Every route needs a logged-in user (see middleware.Auth),
	// and each one has its own per-user rate limit.
	mux.Handle("POST /api/v1/simulations/singleplayer",
		middleware.RateLimit("create", 30, time.Minute,
			http.HandlerFunc(handlers.CreateSingleplayer)))
	mux.Handle("POST /api/v1/simulations/singleplayer/submit",
		middleware.RateLimit("submit", 20, time.Minute,
			http.HandlerFunc(handlers.SubmitSingleplayer)))

	mux.Handle("GET /api/v1/simulations/singleplayer/history",
		middleware.RateLimit("history", 60, time.Minute,
			http.HandlerFunc(handlers.SingleplayerHistory)))

	mux.Handle("GET /api/v1/simulations/singleplayer/limits",
		middleware.RateLimit("limits", 60, time.Minute,
			http.HandlerFunc(handlers.SingleplayerLimits)))

	// Classrooms (institution accounts only; each handler checks that).
	// Faculty create permanent rooms; students join with a code and play games
	// that are charged to the room owner's monthly limit.
	mux.Handle("POST /api/v1/classrooms",
		middleware.RateLimit("classroom-create", 10, time.Minute,
			http.HandlerFunc(handlers.CreateClassroom)))
	mux.Handle("GET /api/v1/classrooms",
		middleware.RateLimit("classroom-list", 60, time.Minute,
			http.HandlerFunc(handlers.ListClassrooms)))
	mux.Handle("POST /api/v1/classrooms/join",
		middleware.RateLimit("classroom-join", 10, time.Minute,
			http.HandlerFunc(handlers.JoinClassroom)))
	mux.Handle("POST /api/v1/classrooms/{id}/start",
		middleware.RateLimit("classroom-start", 30, time.Minute,
			http.HandlerFunc(handlers.StartClassroomGame)))
	mux.Handle("POST /api/v1/classrooms/{id}/submit",
		middleware.RateLimit("classroom-submit", 20, time.Minute,
			http.HandlerFunc(handlers.SubmitClassroomGame)))
	mux.Handle("GET /api/v1/classrooms/{id}/results",
		middleware.RateLimit("classroom-results", 60, time.Minute,
			http.HandlerFunc(handlers.ClassroomResults)))
	mux.Handle("POST /api/v1/classrooms/{id}/archive",
		middleware.RateLimit("classroom-archive", 20, time.Minute,
			http.HandlerFunc(handlers.ArchiveClassroom)))

	// Multiplayer races. Joining and playing are free; creating a room is
	// limited per plan (see handlers/multiplayer_limits.go), on top of this
	// per-minute cap that stops rapid-fire requests.
	mux.Handle("POST /api/v1/rooms",
		middleware.RateLimit("room-create", 5, time.Minute,
			http.HandlerFunc(handlers.CreateRoom)))
	mux.Handle("GET /api/v1/rooms",
		middleware.RateLimit("room-list", 60, time.Minute,
			http.HandlerFunc(handlers.ListRooms)))
	mux.Handle("GET /api/v1/rooms/limits",
		middleware.RateLimit("room-limits", 60, time.Minute,
			http.HandlerFunc(handlers.RoomLimits)))
	mux.Handle("POST /api/v1/rooms/join",
		middleware.RateLimit("room-join", 20, time.Minute,
			http.HandlerFunc(handlers.JoinRoom)))
	// The room page polls this every 2 seconds, so it gets a roomy limit.
	mux.Handle("GET /api/v1/rooms/{code}",
		middleware.RateLimit("room-state", 90, time.Minute,
			http.HandlerFunc(handlers.RoomState)))
	mux.Handle("POST /api/v1/rooms/{code}/start",
		middleware.RateLimit("room-start", 20, time.Minute,
			http.HandlerFunc(handlers.StartRoom)))
	mux.Handle("POST /api/v1/rooms/{code}/leave",
		middleware.RateLimit("room-leave", 20, time.Minute,
			http.HandlerFunc(handlers.LeaveRoom)))
	mux.Handle("POST /api/v1/rooms/{code}/kick",
		middleware.RateLimit("room-kick", 30, time.Minute,
			http.HandlerFunc(handlers.KickPlayer)))
	mux.Handle("POST /api/v1/rooms/{code}/forfeit",
		middleware.RateLimit("room-forfeit", 20, time.Minute,
			http.HandlerFunc(handlers.ForfeitRoom)))
	mux.Handle("POST /api/v1/rooms/{code}/end",
		middleware.RateLimit("room-end", 20, time.Minute,
			http.HandlerFunc(handlers.EndRoom)))
	mux.Handle("GET /api/v1/rooms/{code}/game",
		middleware.RateLimit("room-game", 20, time.Minute,
			http.HandlerFunc(handlers.RoomGame)))
	mux.Handle("POST /api/v1/rooms/{code}/submit",
		middleware.RateLimit("room-submit", 20, time.Minute,
			http.HandlerFunc(handlers.SubmitRoomGame)))

	// CORS answers preflights first (they carry no token), then Auth runs.
	return corsMiddleware(middleware.Auth(mux))
}

// corsMiddleware allows only the origins listed in CORS_ALLOWED_ORIGIN, a
// comma-separated list such as
// "http://127.0.0.1:5500,https://app.example.com". With the variable unset no
// origin is allowed, so browsers block cross-origin calls.
func corsMiddleware(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGIN"), ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			allowed[o] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
