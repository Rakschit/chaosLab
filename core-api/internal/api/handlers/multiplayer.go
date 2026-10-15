package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"core-api/internal/api/middleware"
	"core-api/internal/api/requests"
	"core-api/internal/api/responses"
	"core-api/internal/core"
	"core-api/internal/ratelimit"
	"core-api/internal/store"
	"core-api/services"
)

// Multiplayer races.
//
// A host creates a room; the server picks one incident (seed) for it. Others
// join with the room code. When the host starts, a short countdown runs and
// then everyone gets the same incident and races to submit a fully correct
// answer. The server is the only referee: it keeps the seed, grades every
// submission and measures the time, and the page just polls GET /rooms/{code}.
//
// Creating a room is limited by plan (see multiplayer_limits.go). Joining and
// playing are free and do not touch the singleplayer monthly limits.

const (
	// startCountdown is the pause between "start" and the race clock starting,
	// long enough for every polling page to notice and open the mission.
	startCountdown = 5 * time.Second
	maxPublicRooms = 20
)

// timeLimits is how long a race runs, per difficulty, in seconds.
var timeLimits = map[string]int{"easy": 20 * 60, "medium": 30 * 60, "hard": 45 * 60}

var roomCodePattern = regexp.MustCompile(`^[A-Z0-9]{6}$`)

var userIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// cleanCode accepts "k7q2-x9", "K7Q2X9" or " K7Q2 X9 " and returns "K7Q2X9".
func cleanCode(s string) string {
	s = strings.ToUpper(s)
	s = strings.NewReplacer("-", "", " ", "").Replace(s)
	return s
}

func openRoomsDB(ctx context.Context, w http.ResponseWriter) (*sql.DB, bool) {
	db, err := store.Open(ctx)
	if err != nil {
		log.Printf("rooms: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "multiplayer is temporarily unavailable")
		return nil, false
	}
	return db, true
}

// loadRoomByCode reads {code} from the URL and loads the room.
func loadRoomByCode(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB) (store.Room, bool) {
	code := cleanCode(r.PathValue("code"))
	if !roomCodePattern.MatchString(code) {
		writeError(w, http.StatusNotFound, "room not found")
		return store.Room{}, false
	}
	return loadCode(ctx, w, db, code)
}

func loadCode(ctx context.Context, w http.ResponseWriter, db *sql.DB, code string) (store.Room, bool) {
	room, err := store.GetRoomByCode(ctx, db, code)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "room not found")
		return store.Room{}, false
	}
	if err != nil {
		log.Printf("rooms: load: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load room")
		return store.Room{}, false
	}
	return room, true
}

// requireMember writes a 403 unless the caller is in the room.
func requireMember(ctx context.Context, w http.ResponseWriter, db *sql.DB, room store.Room, userID string) bool {
	ok, err := store.IsRoomMember(ctx, db, room.ID, userID)
	if err != nil {
		log.Printf("rooms: member check: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load room")
		return false
	}
	if !ok {
		if kicked, _ := store.IsKicked(ctx, db, room.ID, userID); kicked {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "the host removed you from this room", "kicked": true})
			return false
		}
		writeError(w, http.StatusForbidden, "join this room first")
		return false
	}
	return true
}

// raceRunning reports whether the clock has started and time is not up.
func raceRunning(room store.Room, now time.Time) bool {
	return room.Status == "playing" && room.StartedAt != nil && !now.Before(*room.StartedAt) &&
		room.EndsAt != nil && now.Before(*room.EndsAt)
}

// CreateRoom makes a room and puts the host in it. Counts toward the plan's
// room-creation limits.
func CreateRoom(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Environment string `json:"environment"`
		Difficulty  string `json:"difficulty"`
		MaxPlayers  int    `json:"max_players"`
		Public      bool   `json:"public"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	in.Difficulty = strings.ToLower(strings.TrimSpace(in.Difficulty))
	limitSecs, okLevel := timeLimits[in.Difficulty]
	if !okLevel {
		writeError(w, http.StatusBadRequest, "difficulty must be easy, medium or hard")
		return
	}
	if in.Environment == "" {
		in.Environment = "nginx"
	}
	if in.Environment != "nginx" {
		writeError(w, http.StatusBadRequest, "unsupported environment")
		return
	}
	if in.MaxPlayers == 0 {
		in.MaxPlayers = 4
	}
	if in.MaxPlayers < 2 || in.MaxPlayers > 8 {
		writeError(w, http.StatusBadRequest, "max players must be 2 to 8")
		return
	}

	plan := middleware.Plan(r.Context())
	lim := roomPlanFor(plan)
	if in.MaxPlayers > lim.MaxPlayers {
		msg := "Your plan allows rooms of up to " + itoa(lim.MaxPlayers) + " players."
		if plan == "free" {
			msg += " Pro allows " + itoa(roomPlans["pro"].MaxPlayers) + "."
		}
		writeError(w, http.StatusForbidden, msg)
		return
	}

	seed, err := core.GenerateSecureSeed()
	if err != nil {
		log.Printf("rooms: generate seed: %v", err)
		writeError(w, http.StatusInternalServerError, "could not create room")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}

	// Count the creation first (atomic, so parallel requests cannot all slip
	// under the limit), and give it back if the room cannot be created.
	refund, ok := chargeRoomCreate(w, r, plan, lim)
	if !ok {
		return
	}

	userID := middleware.UserID(r.Context())
	room, err := store.CreateRoom(ctx, db, store.Room{
		HostID:      userID,
		HostName:    middleware.ProfileOf(r.Context()).DisplayName,
		Environment: in.Environment,
		Difficulty:  in.Difficulty,
		Seed:        seed,
		MaxPlayers:  in.MaxPlayers,
		Public:      in.Public,
		TimeLimit:   limitSecs,
	}, lim.MaxActive)
	if errors.Is(err, store.ErrTooManyRooms) {
		refund()
		msg := "You already have " + itoa(lim.MaxActive) + " open room(s) on the " + plan + " plan. End one or wait for it to finish."
		if plan == "free" {
			msg += " Pro allows " + itoa(roomPlans["pro"].MaxActive) + " at once."
		}
		writeError(w, http.StatusTooManyRequests, msg)
		return
	}
	if err != nil {
		refund()
		log.Printf("rooms: create: %v", err)
		writeError(w, http.StatusInternalServerError, "could not create room")
		return
	}
	writeJSON(w, http.StatusCreated, room)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// RoomLimits reports what the caller may do with rooms and how much is used.
// It only reads the counters.
func RoomLimits(w http.ResponseWriter, r *http.Request) {
	plan := middleware.Plan(r.Context())
	lim := roomPlanFor(plan)
	userID := middleware.UserID(r.Context())
	now := time.Now().UTC()

	type counter struct {
		Limit     int64 `json:"limit"`
		Used      int64 `json:"used"`
		Remaining int64 `json:"remaining"`
	}
	hour := counter{Limit: lim.PerHour, Remaining: lim.PerHour}
	month := counter{Limit: lim.PerMonth, Remaining: lim.PerMonth}
	usageKnown := true

	limiter, err := ratelimit.New()
	if err != nil {
		log.Printf("room limits: counters unavailable: %v", err)
		usageKnown = false
	} else {
		for _, c := range []struct {
			key string
			out *counter
		}{{hourKey(userID, now), &hour}, {roomMonthKey(userID, now), &month}} {
			used, err := limiter.Count(r.Context(), c.key)
			if err != nil {
				log.Printf("room limits: read: %v", err)
				usageKnown = false
				continue
			}
			if used > c.out.Limit {
				used = c.out.Limit
			}
			c.out.Used, c.out.Remaining = used, c.out.Limit-used
		}
	}

	active := 0
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if db, err := store.Open(ctx); err != nil {
		log.Printf("room limits: database unavailable: %v", err)
		usageKnown = false
	} else if active, err = store.CountActiveRooms(ctx, db, userID); err != nil {
		log.Printf("room limits: active rooms: %v", err)
		usageKnown = false
	}

	resp := map[string]any{
		"plan":         plan,
		"usage_known":  usageKnown,
		"hourly":       hour,
		"monthly":      month,
		"max_players":  lim.MaxPlayers,
		"active_rooms": map[string]int{"limit": lim.MaxActive, "used": active},
		"resets_at":    nextMonthStart(now).Format(time.RFC3339),
		"hour_resets":  nextHourStart(now).Format(time.RFC3339),
	}
	if plan == "free" {
		resp["pro_limits"] = roomPlans["pro"]
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// ListRooms shows public rooms that are waiting for players.
func ListRooms(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}
	rooms, err := store.ListPublicRooms(ctx, db, maxPublicRooms)
	if err != nil {
		log.Printf("rooms: list: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load rooms")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

// JoinRoom puts the caller in the room with the given code. Free on every plan.
func JoinRoom(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	code := cleanCode(in.Code)
	if !roomCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, "a room code is 6 letters or digits, like K7Q2-X9")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadCode(ctx, w, db, code)
	if !ok {
		return
	}

	err := store.JoinRoom(ctx, db, room, middleware.UserID(r.Context()), middleware.ProfileOf(r.Context()).DisplayName)
	switch {
	case errors.Is(err, store.ErrRoomFull):
		writeError(w, http.StatusConflict, "this room is full")
		return
	case errors.Is(err, store.ErrNotWaiting):
		writeError(w, http.StatusConflict, "this race has already started")
		return
	case errors.Is(err, store.ErrKicked):
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "the host removed you from this room", "kicked": true})
		return
	case err != nil:
		log.Printf("rooms: join: %v", err)
		writeError(w, http.StatusInternalServerError, "could not join room")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": room.Code})
}

type playerView struct {
	store.Player
	Rank  int  `json:"rank"`
	IsYou bool `json:"is_you"`
	// KickID is only filled in for the host, for every player except the host.
	KickID string `json:"kick_id,omitempty"`
}

// RoomState is what every page polls: the room, the scoreboard and the clock.
func RoomState(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoomByCode(ctx, w, r, db)
	if !ok {
		return
	}
	userID := middleware.UserID(r.Context())
	if !requireMember(ctx, w, db, room, userID) {
		return
	}

	players, err := store.RoomPlayers(ctx, db, room)
	if err != nil {
		log.Printf("rooms: players: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load room")
		return
	}
	views := make([]playerView, 0, len(players))
	var you *playerView
	for i, p := range players {
		v := playerView{Player: p, Rank: i + 1, IsYou: p.UserID == userID}
		if room.HostID == userID && !p.Host {
			v.KickID = p.UserID
		}
		views = append(views, v)
		if v.IsYou {
			c := v
			you = &c
		}
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"room":        room,
		"players":     views,
		"you":         you,
		"is_host":     room.HostID == userID,
		"server_time": time.Now().UTC(),
	})
}

// StartRoom lets the host start the countdown (two players or more).
func StartRoom(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoomByCode(ctx, w, r, db)
	if !ok {
		return
	}
	userID := middleware.UserID(r.Context())
	if room.HostID != userID {
		writeError(w, http.StatusForbidden, "only the host can start the race")
		return
	}

	if err := store.StartRoom(ctx, db, room.ID, userID, startCountdown); err != nil {
		if errors.Is(err, store.ErrCannotStart) {
			msg := "the race cannot be started"
			switch {
			case room.Status != "waiting":
				msg = "this race has already started"
			case room.Players < 2:
				msg = "at least 2 players are needed to start"
			}
			writeError(w, http.StatusConflict, msg)
			return
		}
		log.Printf("rooms: start: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start the race")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"starts_in_seconds": int(startCountdown / time.Second)})
}

// LeaveRoom takes the caller out of a room that has not started. If the host
// leaves, the room is closed.
func LeaveRoom(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoomByCode(ctx, w, r, db)
	if !ok {
		return
	}
	userID := middleware.UserID(r.Context())

	if room.Status == "waiting" {
		var err error
		if room.HostID == userID {
			err = store.EndRoom(ctx, db, room.ID, userID)
			if errors.Is(err, store.ErrNotFound) {
				err = nil
			}
		} else {
			err = store.LeaveRoom(ctx, db, room.ID, userID)
		}
		if err != nil {
			log.Printf("rooms: leave: %v", err)
			writeError(w, http.StatusInternalServerError, "could not leave room")
			return
		}
	}
	// A running race keeps its scoreboard, so leaving then just closes the page.
	writeJSON(w, http.StatusOK, map[string]bool{"left": true})
}

// ForfeitRoom is "End simulation" for one player in a running race. Only that
// player is out; the room and everyone else carry on. Closing the browser does
// not come here, so a dropped player can still rejoin the mission.
func ForfeitRoom(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoomByCode(ctx, w, r, db)
	if !ok {
		return
	}
	userID := middleware.UserID(r.Context())
	if !requireMember(ctx, w, db, room, userID) {
		return
	}
	if room.Status == "playing" {
		if err := store.ForfeitPlayer(ctx, db, room.ID, userID); err != nil {
			log.Printf("rooms: forfeit: %v", err)
			writeError(w, http.StatusInternalServerError, "could not end your simulation")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ended": true})
}

// KickPlayer lets the host remove a player from the room (lobby or running
// race). The removed player cannot rejoin.
func KickPlayer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Player string `json:"player"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target := strings.ToLower(strings.TrimSpace(in.Player))
	if !userIDPattern.MatchString(target) {
		writeError(w, http.StatusBadRequest, "invalid player")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoomByCode(ctx, w, r, db)
	if !ok {
		return
	}
	userID := middleware.UserID(r.Context())
	if room.HostID != userID {
		writeError(w, http.StatusForbidden, "only the host can remove players")
		return
	}
	if target == userID {
		writeError(w, http.StatusBadRequest, "you cannot remove yourself; close the room instead")
		return
	}

	err := store.KickPlayer(ctx, db, room.ID, userID, target)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "that player is not in this room")
		return
	case err != nil:
		log.Printf("rooms: kick: %v", err)
		writeError(w, http.StatusInternalServerError, "could not remove the player")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"kicked": true})
}

// EndRoom lets the host end the race early. Scores stay visible.
func EndRoom(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoomByCode(ctx, w, r, db)
	if !ok {
		return
	}
	if room.HostID != middleware.UserID(r.Context()) {
		writeError(w, http.StatusForbidden, "only the host can end the race")
		return
	}
	if err := store.EndRoom(ctx, db, room.ID, room.HostID); err != nil && !errors.Is(err, store.ErrNotFound) {
		log.Printf("rooms: end: %v", err)
		writeError(w, http.StatusInternalServerError, "could not end the race")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ended": true})
}

type roomSimulation struct {
	responses.SimulationResponse
	RoomCode string    `json:"room_code"`
	EndsAt   time.Time `json:"ends_at"`
}

// RoomGame hands a player the incident, but only once the race clock has
// started. Everyone in the room gets the same one.
func RoomGame(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoomByCode(ctx, w, r, db)
	if !ok {
		return
	}
	if !requireMember(ctx, w, db, room, middleware.UserID(r.Context())) {
		return
	}
	if !raceRunning(room, time.Now().UTC()) {
		writeError(w, http.StatusConflict, "the race is not running")
		return
	}
	if ended, err := store.PlayerEnded(ctx, db, room.ID, middleware.UserID(r.Context())); err != nil {
		log.Printf("rooms: ended check: %v", err)
		writeError(w, http.StatusInternalServerError, "could not open the mission")
		return
	} else if ended {
		writeError(w, http.StatusConflict, "you ended your simulation in this race")
		return
	}

	sim, err := services.BuildSimulation(room.Seed, room.Environment, room.Difficulty)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, roomSimulation{
		SimulationResponse: responses.FromSimulation(sim),
		RoomCode:           room.Code,
		EndsAt:             *room.EndsAt,
	})
}

// SubmitRoomGame grades an answer and records it on the scoreboard. The seed,
// environment and difficulty always come from the room, never from the client.
func SubmitRoomGame(w http.ResponseWriter, r *http.Request) {
	var req requests.SubmitSingleplayerRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	db, ok := openRoomsDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoomByCode(ctx, w, r, db)
	if !ok {
		return
	}
	userID := middleware.UserID(r.Context())
	if !requireMember(ctx, w, db, room, userID) {
		return
	}

	req.Seed, req.Environment, req.Difficulty = room.Seed, room.Environment, room.Difficulty
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !raceRunning(room, time.Now().UTC()) {
		writeError(w, http.StatusConflict, "the race is not running")
		return
	}

	players, err := store.RoomPlayers(ctx, db, room)
	if err != nil {
		log.Printf("rooms: submit players: %v", err)
		writeError(w, http.StatusInternalServerError, "could not grade submission")
		return
	}
	for _, p := range players {
		if p.UserID == userID && p.Solved {
			writeError(w, http.StatusConflict, "you already solved this incident")
			return
		}
		if p.UserID == userID && p.Ended {
			writeError(w, http.StatusConflict, "you ended your simulation in this race")
			return
		}
	}

	// Same per-seed attempt cap as singleplayer, so answers cannot be guessed.
	if !allowAttempt(w, r, room.Seed) {
		return
	}

	result, err := services.ValidateSingleplayerSubmission(req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if err := store.RecordRoomSubmission(ctx, db, room.ID, userID, result.Score, result.Matched, result.Total, result.Correct); err != nil {
		log.Printf("rooms: record submission: %v", err)
		writeError(w, http.StatusServiceUnavailable, "could not save your result, please submit again")
		return
	}
	// Also keep it in this player's history (host included). Best effort.
	saveRoomSubmission(r, room, req, result)
	writeJSON(w, http.StatusOK, result)
}
