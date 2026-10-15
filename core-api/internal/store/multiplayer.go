package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Errors the multiplayer handlers turn into friendly messages.
var (
	ErrTooManyRooms = errors.New("too many open rooms")
	ErrRoomFull     = errors.New("room is full")
	ErrNotWaiting   = errors.New("room is not waiting for players")
	ErrCannotStart  = errors.New("room cannot be started")
	ErrKicked       = errors.New("removed from this room by the host")
)

// Room is one multiplayer race. The seed never leaves the server before the
// race starts, so it is not part of the JSON.
type Room struct {
	ID          string     `json:"-"`
	Code        string     `json:"code"`
	HostID      string     `json:"-"`
	HostName    string     `json:"host_name"`
	Environment string     `json:"environment"`
	Difficulty  string     `json:"difficulty"`
	Seed        string     `json:"-"`
	MaxPlayers  int        `json:"max_players"`
	Public      bool       `json:"public"`
	Status      string     `json:"status"` // waiting, playing or finished
	TimeLimit   int        `json:"time_limit_seconds"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at"`  // when the clock starts (after the countdown)
	EndsAt      *time.Time `json:"ends_at"`     // StartedAt + TimeLimit
	FinishedAt  *time.Time `json:"finished_at"` // when the race ended
	Players     int        `json:"player_count"`
}

// Player is one person in a room.
type Player struct {
	UserID        string `json:"-"`
	Name          string `json:"name"`
	Host          bool   `json:"is_host"`
	Attempts      int    `json:"attempts"`
	BestScore     int    `json:"best_score"`
	Matched       int    `json:"matched"`
	Total         int    `json:"total"`
	Solved        bool   `json:"solved"`
	Ended         bool   `json:"ended"`          // the player pressed "End simulation" and is out of this race
	SolvedSeconds *int   `json:"solved_seconds"` // seconds from the start of the race
}

const roomCols = `r.id::text, r.code, r.host_id::text, r.host_name, r.environment, r.difficulty, r.seed,
	r.max_players, r.is_public, r.status, r.time_limit_s, r.created_at, r.started_at, r.finished_at,
	(SELECT COUNT(*) FROM public.mp_players p WHERE p.room_id = r.id)`

func scanRoom(row scanner) (Room, error) {
	var r Room
	var started, finished sql.NullTime
	if err := row.Scan(&r.ID, &r.Code, &r.HostID, &r.HostName, &r.Environment, &r.Difficulty, &r.Seed,
		&r.MaxPlayers, &r.Public, &r.Status, &r.TimeLimit, &r.CreatedAt, &started, &finished, &r.Players); err != nil {
		return Room{}, err
	}
	if started.Valid {
		t := started.Time.UTC()
		r.StartedAt = &t
		end := t.Add(time.Duration(r.TimeLimit) * time.Second)
		r.EndsAt = &end
	}
	if finished.Valid {
		t := finished.Time.UTC()
		r.FinishedAt = &t
	}
	r.CreatedAt = r.CreatedAt.UTC()
	return r, nil
}

// newRoomCode makes a 6 character code. The alphabet has no I or O, so a
// code can never collide with a fixed route such as /rooms/limits.
func newRoomCode() (string, error) {
	code, err := newJoinCode()
	if err != nil {
		return "", err
	}
	return code[:6], nil
}

// CreateRoom inserts a room and its host as the first player, in one
// statement. It refuses (ErrTooManyRooms) when the host already has maxActive
// rooms that are still open, so the cap holds even for simultaneous requests
// from two tabs.
func CreateRoom(ctx context.Context, db *sql.DB, r Room, maxActive int) (Room, error) {
	for try := 0; try < 5; try++ {
		code, err := newRoomCode()
		if err != nil {
			return Room{}, err
		}
		row := db.QueryRowContext(ctx, `
			WITH ins AS (
				INSERT INTO public.mp_rooms
					(code, host_id, host_name, environment, difficulty, seed, max_players, is_public, time_limit_s)
				SELECT $1, $2::uuid, $3, $4, $5, $6, $7, $8, $9
				WHERE (SELECT COUNT(*) FROM public.mp_rooms h
				        WHERE h.host_id = $2::uuid AND h.status <> 'finished' AND h.expires_at > NOW()) < $10
				RETURNING *
			), host AS (
				INSERT INTO public.mp_players (room_id, user_id, display_name)
				SELECT id, host_id, host_name FROM ins
			)
			SELECT `+roomCols+` FROM ins r`,
			code, r.HostID, r.HostName, r.Environment, r.Difficulty, r.Seed, r.MaxPlayers, r.Public, r.TimeLimit, maxActive)
		out, err := scanRoom(row)
		if errors.Is(err, sql.ErrNoRows) {
			return Room{}, ErrTooManyRooms
		}
		if err == nil {
			out.Players = 1 // the host; the count above cannot see the row inserted in the same statement
			return out, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			continue // code collision: try another
		}
		return Room{}, err
	}
	return Room{}, errors.New("could not allocate a room code")
}

// CountActiveRooms is how many open rooms this host has right now.
func CountActiveRooms(ctx context.Context, db *sql.DB, hostID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM public.mp_rooms
		WHERE host_id = $1::uuid AND status <> 'finished' AND expires_at > NOW()`, hostID).Scan(&n)
	return n, err
}

// finalizeExpired closes rooms whose time is up. Called before every read, so
// no background job is needed.
func finalizeExpired(ctx context.Context, db *sql.DB, where string, arg any) error {
	_, err := db.ExecContext(ctx, `
		UPDATE public.mp_rooms
		SET status = 'finished', finished_at = COALESCE(finished_at, NOW())
		WHERE `+where+` AND status <> 'finished' AND expires_at <= NOW()`, arg)
	return err
}

// GetRoomByCode loads a room by its code (ErrNotFound if there is none).
func GetRoomByCode(ctx context.Context, db *sql.DB, code string) (Room, error) {
	if err := finalizeExpired(ctx, db, "code = $1", code); err != nil {
		return Room{}, err
	}
	r, err := scanRoom(db.QueryRowContext(ctx,
		`SELECT `+roomCols+` FROM public.mp_rooms r WHERE r.code = $1`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return Room{}, ErrNotFound
	}
	return r, err
}

// ListPublicRooms returns public rooms that are still waiting for players.
func ListPublicRooms(ctx context.Context, db *sql.DB, limit int) ([]Room, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+roomCols+` FROM public.mp_rooms r
		WHERE r.is_public AND r.status = 'waiting' AND r.expires_at > NOW()
		ORDER BY r.created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Room{}
	for rows.Next() {
		r, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// IsRoomMember reports whether the user is in the room.
func IsRoomMember(ctx context.Context, db *sql.DB, roomID, userID string) (bool, error) {
	var ok bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM public.mp_players WHERE room_id = $1::uuid AND user_id = $2::uuid)`,
		roomID, userID).Scan(&ok)
	return ok, err
}

// JoinRoom adds a player. Joining again is harmless. The capacity check is
// part of the INSERT, so two people cannot take the last seat together.
func JoinRoom(ctx context.Context, db *sql.DB, room Room, userID, name string) error {
	member, err := IsRoomMember(ctx, db, room.ID, userID)
	if err != nil || member {
		return err
	}
	kicked, err := IsKicked(ctx, db, room.ID, userID)
	if err != nil {
		return err
	}
	if kicked {
		return ErrKicked
	}
	res, err := db.ExecContext(ctx, `
		INSERT INTO public.mp_players (room_id, user_id, display_name)
		SELECT r.id, $2::uuid, $3 FROM public.mp_rooms r
		WHERE r.id = $1::uuid AND r.status = 'waiting' AND r.expires_at > NOW()
		  AND (SELECT COUNT(*) FROM public.mp_players p WHERE p.room_id = r.id) < r.max_players
		ON CONFLICT DO NOTHING`, room.ID, userID, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	if room.Status != "waiting" {
		return ErrNotWaiting
	}
	return ErrRoomFull
}

// IsKicked reports whether the host removed this user from the room.
func IsKicked(ctx context.Context, db *sql.DB, roomID, userID string) (bool, error) {
	var ok bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM public.mp_kicks WHERE room_id = $1::uuid AND user_id = $2::uuid)`,
		roomID, userID).Scan(&ok)
	return ok, err
}

// KickPlayer removes one player from a room that has not finished and bars
// them from joining it again. Host only; the host cannot remove themselves.
// Their answers already saved to their history stay there. If everyone left
// has solved it or ended, the race ends.
func KickPlayer(ctx context.Context, db *sql.DB, roomID, hostID, targetID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		DELETE FROM public.mp_players p USING public.mp_rooms r
		WHERE p.room_id = r.id AND r.id = $1::uuid AND r.host_id = $2::uuid
		  AND p.user_id = $3::uuid AND p.user_id <> r.host_id AND r.status <> 'finished'`,
		roomID, hostID, targetID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO public.mp_kicks (room_id, user_id) VALUES ($1::uuid, $2::uuid)
		ON CONFLICT DO NOTHING`, roomID, targetID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return finishIfNobodyLeftPlaying(ctx, db, roomID)
}

// LeaveRoom removes a player from a room that has not started.
func LeaveRoom(ctx context.Context, db *sql.DB, roomID, userID string) error {
	_, err := db.ExecContext(ctx, `
		DELETE FROM public.mp_players p USING public.mp_rooms r
		WHERE p.room_id = r.id AND r.id = $1::uuid AND p.user_id = $2::uuid AND r.status = 'waiting'`,
		roomID, userID)
	return err
}

// StartRoom starts the countdown. Host only, at least two players, and only
// from the waiting state. The clock starts countdown seconds from now so every
// player's page has time to notice and open the mission together.
func StartRoom(ctx context.Context, db *sql.DB, roomID, hostID string, countdown time.Duration) error {
	res, err := db.ExecContext(ctx, `
		UPDATE public.mp_rooms r
		SET status = 'playing',
		    started_at = NOW() + make_interval(secs => $3),
		    expires_at = NOW() + make_interval(secs => $3 + r.time_limit_s)
		WHERE r.id = $1::uuid AND r.host_id = $2::uuid AND r.status = 'waiting' AND r.expires_at > NOW()
		  AND (SELECT COUNT(*) FROM public.mp_players p WHERE p.room_id = r.id) >= 2`,
		roomID, hostID, int(countdown/time.Second))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrCannotStart
	}
	return nil
}

// EndRoom closes a room. Host only. Scores stay visible.
func EndRoom(ctx context.Context, db *sql.DB, roomID, hostID string) error {
	res, err := db.ExecContext(ctx, `
		UPDATE public.mp_rooms
		SET status = 'finished', finished_at = NOW()
		WHERE id = $1::uuid AND host_id = $2::uuid AND status <> 'finished'`, roomID, hostID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RoomPlayers lists the players, best first: players who solved it by finish
// time, then everyone else by best score.
func RoomPlayers(ctx context.Context, db *sql.DB, room Room) ([]Player, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.user_id::text, p.display_name, (p.user_id = r.host_id),
		       p.attempts, p.best_score, p.matched, p.total, p.solved_at IS NOT NULL, p.ended_at IS NOT NULL,
		       CASE WHEN p.solved_at IS NULL OR r.started_at IS NULL THEN NULL
		            ELSE GREATEST(0, EXTRACT(EPOCH FROM (p.solved_at - r.started_at))::int) END
		FROM public.mp_players p
		JOIN public.mp_rooms r ON r.id = p.room_id
		WHERE p.room_id = $1::uuid
		ORDER BY (p.solved_at IS NULL), p.solved_at, p.best_score DESC, p.matched DESC, p.attempts, p.joined_at`,
		room.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Player{}
	for rows.Next() {
		var p Player
		var secs sql.NullInt64
		if err := rows.Scan(&p.UserID, &p.Name, &p.Host, &p.Attempts, &p.BestScore, &p.Matched, &p.Total, &p.Solved, &p.Ended, &secs); err != nil {
			return nil, err
		}
		if secs.Valid {
			s := int(secs.Int64)
			p.SolvedSeconds = &s
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecordRoomSubmission stores one graded attempt: it keeps the best score and
// sets the finish time on the first fully correct one. When the last player
// solves it, the race ends.
func RecordRoomSubmission(ctx context.Context, db *sql.DB, roomID, userID string, score, matched, total int, correct bool) error {
	_, err := db.ExecContext(ctx, `
		UPDATE public.mp_players
		SET attempts   = attempts + 1,
		    matched    = CASE WHEN $3 > best_score THEN $4 ELSE matched END,
		    best_score = GREATEST(best_score, $3),
		    total      = $5,
		    solved_at  = CASE WHEN $6 AND solved_at IS NULL THEN NOW() ELSE solved_at END
		WHERE room_id = $1::uuid AND user_id = $2::uuid`,
		roomID, userID, score, matched, total, correct)
	if err != nil {
		return err
	}
	if !correct {
		return nil
	}
	return finishIfNobodyLeftPlaying(ctx, db, roomID)
}

// ForfeitPlayer is "End simulation" in a running race: this one player is out
// (their row stays on the scoreboard, marked as ended) and cannot reopen the
// mission or submit again. Everyone else keeps racing. A player who already
// solved it is not affected. Closing the browser or losing the connection does
// NOT call this, so that player can still come back and continue.
func ForfeitPlayer(ctx context.Context, db *sql.DB, roomID, userID string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE public.mp_players p
		SET ended_at = COALESCE(p.ended_at, NOW())
		FROM public.mp_rooms r
		WHERE p.room_id = r.id AND r.id = $1::uuid AND p.user_id = $2::uuid
		  AND r.status = 'playing' AND p.solved_at IS NULL`, roomID, userID)
	if err != nil {
		return err
	}
	return finishIfNobodyLeftPlaying(ctx, db, roomID)
}

// PlayerEnded reports whether this player already ended their own run.
func PlayerEnded(ctx context.Context, db *sql.DB, roomID, userID string) (bool, error) {
	var ended bool
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(ended_at IS NOT NULL, FALSE) FROM public.mp_players
		WHERE room_id = $1::uuid AND user_id = $2::uuid`, roomID, userID).Scan(&ended)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return ended, err
}

// finishIfNobodyLeftPlaying ends the race once every player has either solved
// it or ended their own run.
func finishIfNobodyLeftPlaying(ctx context.Context, db *sql.DB, roomID string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE public.mp_rooms r
		SET status = 'finished', finished_at = NOW()
		WHERE r.id = $1::uuid AND r.status = 'playing'
		  AND NOT EXISTS (SELECT 1 FROM public.mp_players p
		                  WHERE p.room_id = r.id AND p.solved_at IS NULL AND p.ended_at IS NULL)`, roomID)
	return err
}
