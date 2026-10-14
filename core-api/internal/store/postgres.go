// Package store keeps singleplayer history in Postgres (the same database as
// auth-api, but through core-api's own limited login).
package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	mu     sync.Mutex
	shared *sql.DB
)

// withSimpleProtocol makes pgx use the simple query protocol unless the DSN
// already chooses a mode. Transaction-mode poolers (Supabase port 6543,
// PgBouncer, ...) do not keep named prepared statements on one backend, and
// pgx's default statement cache then fails. Errors never include the DSN,
// because it contains the password.
func withSimpleProtocol(dsn string) (string, error) {
	const param = "default_query_exec_mode"

	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", errors.New("DATABASE_URL is not a valid URL")
		}
		q := u.Query()
		if q.Get(param) == "" {
			q.Set(param, "simple_protocol")
			u.RawQuery = q.Encode()
		}
		return u.String(), nil
	}

	if !strings.Contains(dsn, param) {
		dsn += " " + param + "=simple_protocol"
	}
	return dsn, nil
}

// Open returns a process-wide connection pool, creating it on first use.
// Callers must NOT close the returned *sql.DB.
func Open(ctx context.Context) (*sql.DB, error) {
	mu.Lock()
	defer mu.Unlock()

	if shared != nil {
		return shared, nil
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is not configured")
	}

	dsn, err := withSimpleProtocol(dsn)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}

	shared = db
	return shared, nil
}
