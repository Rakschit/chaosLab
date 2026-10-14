package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is returned when a classroom does not exist.
var ErrNotFound = errors.New("not found")

// Classroom is one permanent room owned by a faculty member.
type Classroom struct {
	ID            string           `json:"id"`
	InstitutionID string           `json:"-"`
	OwnerID       string           `json:"-"`
	OwnerName     string           `json:"owner_name"`
	Name          string           `json:"name"`
	JoinCode      string           `json:"join_code,omitempty"`
	Environment   string           `json:"environment"`
	Difficulty    string           `json:"difficulty"`
	MaxAttempts   int              `json:"max_attempts"`
	OwnerLimits   map[string]int64 `json:"-"`
	// SeedMode is "random", "same" or "pool". Seeds holds the fixed seeds for
	// "same" (one) and "pool" (2-3). Students never see the list.
	SeedMode string   `json:"seed_mode"`
	Seeds    []string `json:"-"`
	PoolSize int      `json:"pool_size"`
	Archived bool     `json:"archived"`
	CreatedAt     string           `json:"created_at"`

	// Filled by the list queries only.
	Students int `json:"students"`
	Attempts int `json:"attempts_used"` // joined list: this student's own games
}

const classroomCols = `c.id::text, c.institution_id::text, c.owner_id::text, c.owner_name, c.name,
	c.join_code, c.environment, c.difficulty, c.max_attempts, c.owner_limits::text, c.archived,
	c.seed_mode, c.seeds::text,
	to_char(c.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')`

type scanner interface{ Scan(dest ...any) error }

func scanClassroom(row scanner, extra ...any) (Classroom, error) {
	var c Classroom
	var limits, seeds string
	dest := []any{&c.ID, &c.InstitutionID, &c.OwnerID, &c.OwnerName, &c.Name,
		&c.JoinCode, &c.Environment, &c.Difficulty, &c.MaxAttempts, &limits, &c.Archived,
		&c.SeedMode, &seeds, &c.CreatedAt}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return Classroom{}, err
	}
	c.OwnerLimits = map[string]int64{}
	_ = json.Unmarshal([]byte(limits), &c.OwnerLimits)
	_ = json.Unmarshal([]byte(seeds), &c.Seeds)
	c.PoolSize = len(c.Seeds)
	return c, nil
}

// joinCodeAlphabet leaves out look-alike characters (0/O, 1/I).
const joinCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newJoinCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = joinCodeAlphabet[int(b[i])%len(joinCodeAlphabet)]
	}
	return string(b), nil
}

// CreateClassroom inserts a new room with a fresh join code.
func CreateClassroom(ctx context.Context, db *sql.DB, c Classroom) (Classroom, error) {
	limits, err := json.Marshal(c.OwnerLimits)
	if err != nil {
		return Classroom{}, err
	}
	if c.SeedMode == "" {
		c.SeedMode = "random"
	}
	if c.Seeds == nil {
		c.Seeds = []string{}
	}
	seeds, err := json.Marshal(c.Seeds)
	if err != nil {
		return Classroom{}, err
	}
	for try := 0; try < 5; try++ {
		code, err := newJoinCode()
		if err != nil {
			return Classroom{}, err
		}
		row := db.QueryRowContext(ctx, `
			INSERT INTO public.classrooms AS c
				(institution_id, owner_id, owner_name, name, join_code, environment, difficulty, max_attempts, owner_limits,
				 seed_mode, seeds)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11::jsonb)
			RETURNING `+classroomCols,
			c.InstitutionID, c.OwnerID, c.OwnerName, c.Name, code, c.Environment, c.Difficulty, c.MaxAttempts, string(limits), c.SeedMode, string(seeds))
		out, err := scanClassroom(row)
		if err == nil {
			return out, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			continue // join code collision: try another
		}
		return Classroom{}, err
	}
	return Classroom{}, errors.New("could not allocate a join code")
}

// GetClassroom loads one room by id.
func GetClassroom(ctx context.Context, db *sql.DB, id string) (Classroom, error) {
	c, err := scanClassroom(db.QueryRowContext(ctx,
		`SELECT `+classroomCols+` FROM public.classrooms c WHERE c.id = $1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Classroom{}, ErrNotFound
	}
	return c, err
}

// GetClassroomByCode loads one room by its join code.
func GetClassroomByCode(ctx context.Context, db *sql.DB, code string) (Classroom, error) {
	c, err := scanClassroom(db.QueryRowContext(ctx,
		`SELECT `+classroomCols+` FROM public.classrooms c WHERE c.join_code = $1`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return Classroom{}, ErrNotFound
	}
	return c, err
}

// RefreshOwner keeps the owner's name and monthly limits on all of their
// rooms current. Student games are charged against these limits.
func RefreshOwner(ctx context.Context, db *sql.DB, ownerID, name string, limits map[string]int64) error {
	raw, err := json.Marshal(limits)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		UPDATE public.classrooms SET owner_name = $2, owner_limits = $3::jsonb WHERE owner_id = $1::uuid
	`, ownerID, name, string(raw))
	return err
}

// ListOwned returns the rooms a faculty member created, newest first.
func ListOwned(ctx context.Context, db *sql.DB, ownerID string) ([]Classroom, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+classroomCols+`,
		       (SELECT COUNT(*) FROM public.classroom_members m WHERE m.classroom_id = c.id)
		FROM public.classrooms c
		WHERE c.owner_id = $1::uuid
		ORDER BY c.created_at DESC
		LIMIT 200
	`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Classroom{}
	for rows.Next() {
		var students int
		c, err := scanClassroom(rows, &students)
		if err != nil {
			return nil, err
		}
		c.Students = students
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListJoined returns the rooms a person joined, with how many games they used.
// The join code is not included: only the owner needs it.
func ListJoined(ctx context.Context, db *sql.DB, userID string) ([]Classroom, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+classroomCols+`,
		       (SELECT COUNT(*) FROM public.singleplayer_runs r
		         WHERE r.classroom_id = c.id AND r.user_id = $1::uuid)
		FROM public.classroom_members mm
		JOIN public.classrooms c ON c.id = mm.classroom_id
		WHERE mm.user_id = $1::uuid
		ORDER BY mm.joined_at DESC
		LIMIT 200
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Classroom{}
	for rows.Next() {
		var used int
		c, err := scanClassroom(rows, &used)
		if err != nil {
			return nil, err
		}
		c.JoinCode = ""
		c.Attempts = used
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddMember joins a person to a room (joining twice is fine).
func AddMember(ctx context.Context, db *sql.DB, classroomID, userID, name, email string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO public.classroom_members (classroom_id, user_id, display_name, email)
		VALUES ($1::uuid, $2::uuid, $3, $4)
		ON CONFLICT (classroom_id, user_id) DO NOTHING
	`, classroomID, userID, name, email)
	return err
}

// IsMember reports whether the person joined the room.
func IsMember(ctx context.Context, db *sql.DB, classroomID, userID string) (bool, error) {
	var one int
	err := db.QueryRowContext(ctx, `
		SELECT 1 FROM public.classroom_members WHERE classroom_id = $1::uuid AND user_id = $2::uuid
	`, classroomID, userID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// CountAttempts is how many games this student has started in the room.
func CountAttempts(ctx context.Context, db *sql.DB, classroomID, userID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM public.singleplayer_runs WHERE classroom_id = $1::uuid AND user_id = $2::uuid
	`, classroomID, userID).Scan(&n)
	return n, err
}

// StudentSeeds lists the seeds this student already started in the room.
func StudentSeeds(ctx context.Context, db *sql.DB, classroomID, userID string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seed FROM public.singleplayer_runs WHERE classroom_id = $1::uuid AND user_id = $2::uuid
	`, classroomID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// HasRun reports whether this student started a game with this seed in the room.
func HasRun(ctx context.Context, db *sql.DB, classroomID, userID, seed string) (bool, error) {
	var one int
	err := db.QueryRowContext(ctx, `
		SELECT 1 FROM public.singleplayer_runs
		WHERE classroom_id = $1::uuid AND user_id = $2::uuid AND seed = $3
	`, classroomID, userID, seed).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// SetArchived archives or reopens a room. Only the owner's room is touched.
func SetArchived(ctx context.Context, db *sql.DB, classroomID, ownerID string, archived bool) error {
	res, err := db.ExecContext(ctx, `
		UPDATE public.classrooms SET archived = $3 WHERE id = $1::uuid AND owner_id = $2::uuid
	`, classroomID, ownerID, archived)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// StudentRow is one student's best result in a room.
type StudentRow struct {
	UserID          string `json:"user_id"`
	DisplayName     string `json:"display_name"`
	Email           string `json:"email"`
	GamesStarted    int    `json:"games_started"`
	Submissions     int    `json:"submissions"`
	BestScore       *int   `json:"best_score"`
	BestCorrect     *bool  `json:"best_correct"`
	BestDuration    *int   `json:"best_duration_seconds"`
	LastSubmittedAt string `json:"last_submitted_at"`
}

// SubmissionRow is one graded submission in a room.
type SubmissionRow struct {
	DisplayName     string `json:"display_name"`
	Email           string `json:"email"`
	AttemptNumber   int    `json:"attempt_number"`
	Seed            string `json:"seed"`
	Score           int    `json:"score"`
	Matched         int    `json:"matched"`
	Total           int    `json:"total"`
	Correct         bool   `json:"correct"`
	DurationSeconds *int   `json:"duration_seconds"`
	CreatedAt       string `json:"created_at"`
}

// ClassroomResults returns every student with their best score and time, plus
// the latest submissions. Best = highest score, then the shortest time.
func ClassroomResults(ctx context.Context, db *sql.DB, classroomID string) ([]StudentRow, []SubmissionRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT m.user_id::text, m.display_name, m.email,
		       (SELECT COUNT(*) FROM public.singleplayer_runs r
		         WHERE r.classroom_id = m.classroom_id AND r.user_id = m.user_id),
		       (SELECT COUNT(*) FROM public.singleplayer_results x
		         WHERE x.classroom_id = m.classroom_id AND x.user_id = m.user_id),
		       b.score, b.correct, b.duration_seconds,
		       COALESCE(to_char(l.last_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')
		FROM public.classroom_members m
		LEFT JOIN LATERAL (
			SELECT x.score, x.correct, x.duration_seconds
			FROM public.singleplayer_results x
			WHERE x.classroom_id = m.classroom_id AND x.user_id = m.user_id
			ORDER BY x.score DESC, x.duration_seconds ASC NULLS LAST, x.created_at ASC
			LIMIT 1
		) b ON TRUE
		LEFT JOIN LATERAL (
			SELECT MAX(x.created_at) AS last_at FROM public.singleplayer_results x
			WHERE x.classroom_id = m.classroom_id AND x.user_id = m.user_id
		) l ON TRUE
		WHERE m.classroom_id = $1::uuid
		ORDER BY b.score DESC NULLS LAST, b.duration_seconds ASC NULLS LAST, m.display_name
		LIMIT 1000
	`, classroomID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	students := []StudentRow{}
	for rows.Next() {
		var s StudentRow
		var score, duration sql.NullInt64
		var correct sql.NullBool
		if err := rows.Scan(&s.UserID, &s.DisplayName, &s.Email, &s.GamesStarted, &s.Submissions,
			&score, &correct, &duration, &s.LastSubmittedAt); err != nil {
			return nil, nil, err
		}
		if score.Valid {
			v := int(score.Int64)
			s.BestScore = &v
		}
		if correct.Valid {
			v := correct.Bool
			s.BestCorrect = &v
		}
		if duration.Valid {
			v := int(duration.Int64)
			s.BestDuration = &v
		}
		students = append(students, s)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	subRows, err := db.QueryContext(ctx, `
		SELECT m.display_name, m.email, x.attempt_number, x.seed, x.score, x.matched, x.total,
		       x.correct, x.duration_seconds,
		       to_char(x.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM public.singleplayer_results x
		JOIN public.classroom_members m ON m.classroom_id = x.classroom_id AND m.user_id = x.user_id
		WHERE x.classroom_id = $1::uuid
		ORDER BY x.created_at DESC
		LIMIT 500
	`, classroomID)
	if err != nil {
		return nil, nil, err
	}
	defer subRows.Close()

	subs := []SubmissionRow{}
	for subRows.Next() {
		var s SubmissionRow
		var duration sql.NullInt64
		if err := subRows.Scan(&s.DisplayName, &s.Email, &s.AttemptNumber, &s.Seed, &s.Score,
			&s.Matched, &s.Total, &s.Correct, &duration, &s.CreatedAt); err != nil {
			return nil, nil, err
		}
		if duration.Valid {
			v := int(duration.Int64)
			s.DurationSeconds = &v
		}
		subs = append(subs, s)
	}
	if err := subRows.Err(); err != nil {
		return nil, nil, err
	}
	return students, subs, nil
}
