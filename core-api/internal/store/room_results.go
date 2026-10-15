package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// SaveRoomResult stores one graded multiplayer answer in the same history table
// as singleplayer answers, tagged with the room code. The time taken is
// measured from the moment the race clock started, not from when the player
// opened the mission. Returns the attempt number.
func SaveRoomResult(ctx context.Context, db *sql.DB, res Result, roomCode string, raceStart time.Time) (int, error) {
	findings, err := json.Marshal(res.Findings)
	if err != nil {
		return 0, err
	}
	if res.Findings == nil {
		findings = []byte("[]")
	}

	const query = `
		INSERT INTO public.singleplayer_results
			(user_id, seed, environment, difficulty, attempt_number,
			 score, matched, total, correct, findings, started_at, duration_seconds, room_code)
		VALUES ($1::uuid, $2, $3, $4,
			COALESCE((SELECT MAX(attempt_number) FROM public.singleplayer_results
			          WHERE user_id = $1::uuid AND seed = $2), 0) + 1,
			$5, $6, $7, $8, $9::jsonb,
			$10::timestamptz,
			GREATEST(0, EXTRACT(EPOCH FROM (NOW() - $10::timestamptz))::int),
			$11)
		RETURNING attempt_number`

	for try := 0; try < maxSaveAttempts; try++ {
		var attempt int
		err = db.QueryRowContext(ctx, query,
			res.UserID, res.Seed, res.Environment, res.Difficulty,
			res.Score, res.Matched, res.Total, res.Correct, string(findings),
			raceStart.UTC(), roomCode,
		).Scan(&attempt)
		if err == nil {
			return attempt, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			continue
		}
		return 0, err
	}
	return 0, fmt.Errorf("could not allocate an attempt number: %w", err)
}
