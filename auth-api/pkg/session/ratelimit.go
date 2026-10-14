package session

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

const (
	rateLimitPrefix  = "auth:rl:"
	userSessionsPref = "auth:user_sessions:"
)

// Hit counts one event for key inside a fixed window and returns the new
// count. The window starts at the first hit and the key then expires on its
// own, so nothing needs cleaning up.
func (r *Redis) Hit(ctx context.Context, key string, window time.Duration) (int64, error) {
	k := rateLimitPrefix + key
	res, err := r.command(ctx, "INCR", k)
	if err != nil {
		return 0, err
	}
	var n int64
	if err := json.Unmarshal(res, &n); err != nil {
		return 0, errors.New("invalid counter value")
	}
	if n == 1 {
		if _, err := r.command(ctx, "EXPIRE", k, int64(window/time.Second)); err != nil {
			// Never leave a counter behind that cannot expire.
			_, _ = r.command(ctx, "DEL", k)
			return 0, err
		}
	}
	return n, nil
}

// Count returns the current count for key without changing it (0 if unset).
func (r *Redis) Count(ctx context.Context, key string) (int64, error) {
	res, err := r.command(ctx, "GET", rateLimitPrefix+key)
	if err != nil {
		return 0, err
	}
	if len(res) == 0 || string(res) == "null" {
		return 0, nil
	}
	var s string
	if err := json.Unmarshal(res, &s); err != nil {
		return 0, errors.New("invalid counter value")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errors.New("invalid counter value")
	}
	return n, nil
}

// RetryAfter returns how long until the counter for key resets (0 if unknown).
func (r *Redis) RetryAfter(ctx context.Context, key string) time.Duration {
	res, err := r.command(ctx, "TTL", rateLimitPrefix+key)
	if err != nil {
		return 0
	}
	var secs int64
	if err := json.Unmarshal(res, &secs); err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// Reset clears the counter for key.
func (r *Redis) Reset(ctx context.Context, key string) error {
	_, err := r.command(ctx, "DEL", rateLimitPrefix+key)
	return err
}

// CreateSession stores a session like Set and also records it in a per-user
// index, so every session of a user can be revoked at once.
func (r *Redis) CreateSession(ctx context.Context, tokenHash, userID string, ttl time.Duration) error {
	if err := r.Set(ctx, tokenHash, userID, ttl); err != nil {
		return err
	}
	idx := userSessionsPref + userID
	if _, err := r.command(ctx, "SADD", idx, tokenHash); err != nil {
		_ = r.Delete(ctx, tokenHash)
		return err
	}
	if _, err := r.command(ctx, "EXPIRE", idx, int64(ttl/time.Second)); err != nil {
		_ = r.Delete(ctx, tokenHash)
		return err
	}
	return nil
}

// DeleteUserSessions revokes every session belonging to the user.
func (r *Redis) DeleteUserSessions(ctx context.Context, userID string) error {
	idx := userSessionsPref + userID
	res, err := r.command(ctx, "SMEMBERS", idx)
	if err != nil {
		return err
	}
	var hashes []string
	if len(res) > 0 && string(res) != "null" {
		if err := json.Unmarshal(res, &hashes); err != nil {
			return errors.New("invalid session index")
		}
	}
	args := []any{"DEL", idx}
	for _, h := range hashes {
		args = append(args, "auth:session:"+h)
	}
	_, err = r.command(ctx, args...)
	return err
}
