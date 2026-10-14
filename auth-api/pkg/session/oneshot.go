package session

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const oneShotPrefix = "auth:once:"

// PutOnce stores a short-lived value under key. It is used for single-use
// login "state" values: the value can be taken back exactly once.
func (r *Redis) PutOnce(ctx context.Context, key, value string, ttl time.Duration) error {
	if key == "" || value == "" {
		return errors.New("key and value are required")
	}
	if ttl < time.Second {
		return errors.New("TTL must be at least one second")
	}
	_, err := r.command(ctx, "SET", oneShotPrefix+key, value, "EX", int64(ttl/time.Second))
	return err
}

// TakeOnce returns the value stored under key and deletes it in the same
// step, so a second call (a replay) gets "". A missing or expired key also
// returns "".
func (r *Redis) TakeOnce(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", nil
	}
	res, err := r.command(ctx, "GETDEL", oneShotPrefix+key)
	if err != nil {
		return "", err
	}
	if len(res) == 0 || string(res) == "null" {
		return "", nil
	}
	var v string
	if err := json.Unmarshal(res, &v); err != nil {
		return "", errors.New("invalid stored value")
	}
	return v, nil
}
