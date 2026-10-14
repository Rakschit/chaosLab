package handlers

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"core-api/internal/api/middleware"
	"core-api/internal/api/requests"
	"core-api/internal/api/responses"
	"core-api/internal/core"
	"core-api/internal/ratelimit"
	"core-api/internal/store"
	"core-api/services"
)

// Classroom mode.
//
// A faculty member creates a room once; it never expires (it can only be
// archived). Students join with a code and play games on the room's difficulty.
// Every student game is charged to the ROOM OWNER's monthly limit for that
// difficulty (the same counter the owner's own singleplayer games use), and
// never to the student's personal limit. The owner's limit is copied onto the
// room each time the owner opens the classroom page, because a student's
// request only knows the student's limits.

const (
	maxRoomsPerOwner = 50
	maxRoomNameRunes = 80
)

var (
	uuidPattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	joinCodePattern = regexp.MustCompile(`^[A-Z0-9]{8}$`)
	levels          = []string{"easy", "medium", "hard"}
)

// institutionUser returns the caller's institution, or writes a 403.
func institutionUser(w http.ResponseWriter, r *http.Request) (*middleware.Institution, bool) {
	inst := middleware.InstitutionOf(r.Context())
	if middleware.Plan(r.Context()) != "institution" || inst == nil {
		writeError(w, http.StatusForbidden, "Classrooms are only available to institution accounts.")
		return nil, false
	}
	return inst, true
}

func openDB(ctx context.Context, w http.ResponseWriter) (*sql.DB, bool) {
	db, err := store.Open(ctx)
	if err != nil {
		log.Printf("classroom: database unavailable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "classrooms are temporarily unavailable")
		return nil, false
	}
	return db, true
}

// loadRoom finds the room named in the URL. Rooms of other institutions look
// like they do not exist.
func loadRoom(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *middleware.Institution) (store.Classroom, bool) {
	id := strings.ToLower(r.PathValue("id"))
	if !uuidPattern.MatchString(id) {
		writeError(w, http.StatusNotFound, "classroom not found")
		return store.Classroom{}, false
	}
	room, err := store.GetClassroom(ctx, db, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && room.InstitutionID != inst.ID) {
		writeError(w, http.StatusNotFound, "classroom not found")
		return store.Classroom{}, false
	}
	if err != nil {
		log.Printf("classroom: load: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load classroom")
		return store.Classroom{}, false
	}
	return room, true
}

func copyLimits(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// usage reads how much of a user's monthly allowance is used, per difficulty.
func usage(ctx context.Context, userID string, limits map[string]int64, now time.Time) (map[string]limitStatus, bool) {
	out := make(map[string]limitStatus, len(levels))
	known := true
	limiter, err := ratelimit.New()
	if err != nil {
		log.Printf("classroom: counters unavailable: %v", err)
		limiter, known = nil, false
	}
	for _, level := range levels {
		limit := limits[level]
		st := limitStatus{Limit: limit, Remaining: limit}
		if limiter != nil {
			used, err := limiter.Count(ctx, monthKey(userID, level, now))
			if err != nil {
				log.Printf("classroom: read %s: %v", level, err)
				known = false
			} else {
				if used > limit {
					used = limit
				}
				st.Used, st.Remaining = used, limit-used
			}
		}
		out[level] = st
	}
	return out, known
}

// CreateClassroom makes a new permanent room. Faculty only.
func CreateClassroom(w http.ResponseWriter, r *http.Request) {
	inst, ok := institutionUser(w, r)
	if !ok {
		return
	}
	if inst.MemberRole != "faculty" {
		writeError(w, http.StatusForbidden, "Only faculty can create a classroom.")
		return
	}

	var in struct {
		Name        string `json:"name"`
		Difficulty  string `json:"difficulty"`
		MaxAttempts int    `json:"max_attempts"`
		SeedMode    string `json:"seed_mode"` // random (default), same or pool
		PoolSize    int    `json:"pool_size"` // 2 or 3, only for pool
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(name); n < 2 || n > maxRoomNameRunes {
		writeError(w, http.StatusBadRequest, "name must be 2 to 80 characters")
		return
	}
	if in.Difficulty != "easy" && in.Difficulty != "medium" && in.Difficulty != "hard" {
		writeError(w, http.StatusBadRequest, "difficulty must be easy, medium or hard")
		return
	}
	if in.MaxAttempts == 0 {
		in.MaxAttempts = 1
	}
	if in.MaxAttempts < 1 || in.MaxAttempts > 10 {
		writeError(w, http.StatusBadRequest, "attempts per student must be 1 to 10")
		return
	}

	if in.SeedMode == "" {
		in.SeedMode = "random"
	}
	var seeds []string
	switch in.SeedMode {
	case "random":
	case "same":
		seeds = make([]string, 0, 1)
		in.MaxAttempts = 1 // one seed means one game per student
	case "pool":
		if in.PoolSize < 2 || in.PoolSize > 3 {
			writeError(w, http.StatusBadRequest, "a seed pool has 2 or 3 seeds")
			return
		}
		seeds = make([]string, 0, in.PoolSize)
		// A student never replays a seed, so they cannot have more games than seeds.
		if in.MaxAttempts > in.PoolSize {
			in.MaxAttempts = in.PoolSize
		}
	default:
		writeError(w, http.StatusBadRequest, "seed_mode must be random, same or pool")
		return
	}
	want := 0
	switch in.SeedMode {
	case "same":
		want = 1
	case "pool":
		want = in.PoolSize
	}
	for len(seeds) < want {
		seed, err := core.GenerateSecureSeed()
		if err != nil {
			log.Printf("classroom: generate seed: %v", err)
			writeError(w, http.StatusInternalServerError, "could not create classroom")
			return
		}
		seeds = append(seeds, seed)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openDB(ctx, w)
	if !ok {
		return
	}

	userID := middleware.UserID(r.Context())
	owned, err := store.ListOwned(ctx, db, userID)
	if err != nil {
		log.Printf("classroom: count owned: %v", err)
		writeError(w, http.StatusInternalServerError, "could not create classroom")
		return
	}
	if len(owned) >= maxRoomsPerOwner {
		writeError(w, http.StatusBadRequest, "you have reached the maximum number of classrooms; archive one you no longer use")
		return
	}

	limits := copyLimits(inst.MonthlyLimits)
	room, err := store.CreateClassroom(ctx, db, store.Classroom{
		InstitutionID: inst.ID,
		OwnerID:       userID,
		OwnerName:     middleware.ProfileOf(r.Context()).DisplayName,
		Name:          name,
		Environment:   "nginx",
		Difficulty:    in.Difficulty,
		MaxAttempts:   in.MaxAttempts,
		OwnerLimits:   limits,
		SeedMode:      in.SeedMode,
		Seeds:         seeds,
	})
	if err != nil {
		log.Printf("classroom: create: %v", err)
		writeError(w, http.StatusInternalServerError, "could not create classroom")
		return
	}
	writeJSON(w, http.StatusCreated, room)
}

// ListClassrooms returns the rooms the caller owns (faculty) and the rooms
// they joined, plus the faculty member's remaining monthly allowance.
func ListClassrooms(w http.ResponseWriter, r *http.Request) {
	inst, ok := institutionUser(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openDB(ctx, w)
	if !ok {
		return
	}

	userID := middleware.UserID(r.Context())
	now := time.Now().UTC()
	resp := map[string]any{
		"member_role": inst.MemberRole,
		"resets_at":   nextMonthStart(now).Format(time.RFC3339),
	}

	if inst.MemberRole == "faculty" {
		limits := copyLimits(inst.MonthlyLimits)
		if err := store.RefreshOwner(ctx, db, userID, middleware.ProfileOf(r.Context()).DisplayName, limits); err != nil {
			log.Printf("classroom: refresh owner: %v", err)
		}
		owned, err := store.ListOwned(ctx, db, userID)
		if err != nil {
			log.Printf("classroom: list owned: %v", err)
			writeError(w, http.StatusInternalServerError, "could not load classrooms")
			return
		}
		resp["owned"] = owned
		allowance, known := usage(ctx, userID, limits, now)
		resp["allowance"] = allowance
		resp["usage_known"] = known
	}

	joined, err := store.ListJoined(ctx, db, userID)
	if err != nil {
		log.Printf("classroom: list joined: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load classrooms")
		return
	}
	mine := make([]store.Classroom, 0, len(joined))
	for _, c := range joined {
		if c.InstitutionID == inst.ID {
			mine = append(mine, c)
		}
	}
	resp["joined"] = mine

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// JoinClassroom adds the caller to the room with the given code.
func JoinClassroom(w http.ResponseWriter, r *http.Request) {
	inst, ok := institutionUser(w, r)
	if !ok {
		return
	}

	var in struct {
		Code string `json:"code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if !joinCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, "a join code is 8 letters or digits")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openDB(ctx, w)
	if !ok {
		return
	}

	room, err := store.GetClassroomByCode(ctx, db, code)
	// Same answer for "no such code" and "another institution's room".
	if errors.Is(err, store.ErrNotFound) || (err == nil && room.InstitutionID != inst.ID) {
		writeError(w, http.StatusNotFound, "no classroom with that code")
		return
	}
	if err != nil {
		log.Printf("classroom: join lookup: %v", err)
		writeError(w, http.StatusInternalServerError, "could not join classroom")
		return
	}
	if room.Archived {
		writeError(w, http.StatusBadRequest, "this classroom is archived")
		return
	}

	profile := middleware.ProfileOf(r.Context())
	if err := store.AddMember(ctx, db, room.ID, middleware.UserID(r.Context()), profile.DisplayName, profile.Email); err != nil {
		log.Printf("classroom: join: %v", err)
		writeError(w, http.StatusInternalServerError, "could not join classroom")
		return
	}
	room.JoinCode = ""
	writeJSON(w, http.StatusOK, room)
}

type classroomSimulation struct {
	responses.SimulationResponse
	ClassroomID string `json:"classroom_id"`
}

// StartClassroomGame starts one game for a student. The seed is always chosen
// by the server. One game is charged to the room owner's monthly limit.
func StartClassroomGame(w http.ResponseWriter, r *http.Request) {
	inst, ok := institutionUser(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	db, ok := openDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoom(ctx, w, r, db, inst)
	if !ok {
		return
	}

	userID := middleware.UserID(r.Context())
	member, err := store.IsMember(ctx, db, room.ID, userID)
	if err != nil {
		log.Printf("classroom: member check: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start game")
		return
	}
	if !member {
		writeError(w, http.StatusForbidden, "join this classroom first")
		return
	}
	if room.Archived {
		writeError(w, http.StatusForbidden, "this classroom is archived")
		return
	}

	used, err := store.CountAttempts(ctx, db, room.ID, userID)
	if err != nil {
		log.Printf("classroom: count attempts: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start game")
		return
	}
	if used >= room.MaxAttempts {
		writeError(w, http.StatusForbidden, "you have used all "+strconv.Itoa(room.MaxAttempts)+" attempt(s) in this classroom")
		return
	}

	seed, ok := pickSeed(ctx, w, db, room, userID)
	if !ok {
		return
	}

	// An empty seed makes the server generate a fresh random one.
	simulation, err := services.CreateSingleplayer(requests.CreateSingleplayerRequest{
		Seed:        seed,
		Environment: room.Environment,
		Difficulty:  room.Difficulty,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	if !chargeOwner(w, r, room) {
		return
	}

	if err := store.RecordStart(ctx, db, userID, simulation.Seed, simulation.Environment, simulation.Difficulty, room.ID); err != nil {
		log.Printf("classroom: record start: %v", err)
		writeError(w, http.StatusServiceUnavailable, "could not start game, please try again")
		return
	}

	writeJSON(w, http.StatusOK, classroomSimulation{
		SimulationResponse: responses.FromSimulation(simulation),
		ClassroomID:        room.ID,
	})
}

// pickSeed chooses the seed for a student's next game. "random" rooms return ""
// (the server then generates one). "same" and "pool" rooms hand out one of the
// room's fixed seeds that this student has not played yet.
func pickSeed(ctx context.Context, w http.ResponseWriter, db *sql.DB, room store.Classroom, userID string) (string, bool) {
	if (room.SeedMode != "same" && room.SeedMode != "pool") || len(room.Seeds) == 0 {
		return "", true
	}

	taken, err := store.StudentSeeds(ctx, db, room.ID, userID)
	if err != nil {
		log.Printf("classroom: student seeds: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start game")
		return "", false
	}
	played := make(map[string]bool, len(taken))
	for _, s := range taken {
		played[s] = true
	}

	available := make([]string, 0, len(room.Seeds))
	for _, s := range room.Seeds {
		if !played[s] {
			available = append(available, s)
		}
	}
	if len(available) == 0 {
		writeError(w, http.StatusForbidden, "you have already played every incident in this classroom")
		return "", false
	}
	return available[rand.IntN(len(available))], true
}

// chargeOwner counts one game against the room owner's monthly limit for the
// room's difficulty. It returns false after writing a 429 when it is used up.
// If Redis is unavailable it logs and allows the game, like allowStart.
func chargeOwner(w http.ResponseWriter, r *http.Request, room store.Classroom) bool {
	limit, ok := room.OwnerLimits[room.Difficulty]
	if !ok {
		return true
	}

	limiter, err := ratelimit.New()
	if err != nil {
		log.Printf("classroom limit: disabled: %v", err)
		return true
	}

	now := time.Now().UTC()
	n, err := limiter.Hit(r.Context(), monthKey(room.OwnerID, room.Difficulty, now), 40*24*time.Hour)
	if err != nil {
		log.Printf("classroom limit: %v", err)
		return true
	}

	if n > limit {
		w.Header().Set("Retry-After", strconv.Itoa(int(nextMonthStart(now).Sub(now).Seconds())))
		writeError(w, http.StatusTooManyRequests,
			"This classroom has used up its monthly "+room.Difficulty+" games. Ask your faculty; it resets on the 1st at 00:00 UTC.")
		return false
	}
	return true
}

// SubmitClassroomGame grades a classroom game and saves it to the room.
func SubmitClassroomGame(w http.ResponseWriter, r *http.Request) {
	inst, ok := institutionUser(w, r)
	if !ok {
		return
	}

	var req requests.SubmitSingleplayerRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	db, ok := openDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoom(ctx, w, r, db, inst)
	if !ok {
		return
	}

	// Environment and difficulty come from the room, never from the client.
	req.Environment = room.Environment
	req.Difficulty = room.Difficulty
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	seed := strings.ToLower(strings.TrimSpace(req.Seed))
	if b, err := hex.DecodeString(seed); err != nil || len(b) != 8 {
		writeError(w, http.StatusBadRequest, "invalid seed: must be 16 hex characters")
		return
	}

	userID := middleware.UserID(r.Context())
	started, err := store.HasRun(ctx, db, room.ID, userID, seed)
	if err != nil {
		log.Printf("classroom: run check: %v", err)
		writeError(w, http.StatusInternalServerError, "could not grade submission")
		return
	}
	if !started {
		writeError(w, http.StatusBadRequest, "that game was not started in this classroom")
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

	if err := saveSubmission(r, seed, room.ID, req, result); err != nil {
		writeError(w, http.StatusServiceUnavailable, "could not save your result, please submit again")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ClassroomResults shows the owner every student's score and time taken.
func ClassroomResults(w http.ResponseWriter, r *http.Request) {
	inst, ok := institutionUser(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	db, ok := openDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoom(ctx, w, r, db, inst)
	if !ok {
		return
	}
	if room.OwnerID != middleware.UserID(r.Context()) {
		writeError(w, http.StatusForbidden, "only the owner can see classroom results")
		return
	}

	students, submissions, err := store.ClassroomResults(ctx, db, room.ID)
	if err != nil {
		log.Printf("classroom: results: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load results")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"classroom":   room,
		"students":    students,
		"submissions": submissions,
	})
}

// ArchiveClassroom archives or reopens a room. Owner only.
func ArchiveClassroom(w http.ResponseWriter, r *http.Request) {
	inst, ok := institutionUser(w, r)
	if !ok {
		return
	}

	var in struct {
		Archived bool `json:"archived"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	db, ok := openDB(ctx, w)
	if !ok {
		return
	}
	room, ok := loadRoom(ctx, w, r, db, inst)
	if !ok {
		return
	}

	err := store.SetArchived(ctx, db, room.ID, middleware.UserID(r.Context()), in.Archived)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusForbidden, "only the owner can archive a classroom")
		return
	}
	if err != nil {
		log.Printf("classroom: archive: %v", err)
		writeError(w, http.StatusInternalServerError, "could not update classroom")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"archived": in.Archived})
}
