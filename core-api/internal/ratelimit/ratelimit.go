// Package ratelimit counts events in fixed windows using Upstash Redis (REST).
// Redis is used instead of process memory because Vercel runs several
// short-lived instances, so an in-memory counter would not be shared.
package ratelimit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const keyPrefix = "core:rl:"

type Limiter struct {
	url    string
	token  string
	client *http.Client
}

type redisResponse struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// New reads UPSTASH_REDIS_REST_URL and UPSTASH_REDIS_REST_TOKEN.
func New() (*Limiter, error) {
	url := strings.TrimRight(strings.TrimSpace(os.Getenv("UPSTASH_REDIS_REST_URL")), "/")
	token := strings.TrimSpace(os.Getenv("UPSTASH_REDIS_REST_TOKEN"))

	if url == "" || token == "" {
		return nil, errors.New("upstash redis configuration is missing")
	}
	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("upstash redis URL must use HTTPS")
	}

	return &Limiter{
		url:    url,
		token:  token,
		client: &http.Client{Timeout: 3 * time.Second},
	}, nil
}

func (l *Limiter) command(ctx context.Context, args ...any) (json.RawMessage, error) {
	body, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstash request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstash returned HTTP %d", resp.StatusCode)
	}

	var out redisResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.New("invalid upstash response")
	}
	if out.Error != "" {
		return nil, fmt.Errorf("upstash command failed: %s", out.Error)
	}
	return out.Result, nil
}

// Hit counts one event for key inside a fixed window and returns the new
// count. The window starts at the first hit; the key then expires on its own.
func (l *Limiter) Hit(ctx context.Context, key string, window time.Duration) (int64, error) {
	if window < time.Second {
		return 0, errors.New("window must be at least one second")
	}

	k := keyPrefix + key
	res, err := l.command(ctx, "INCR", k)
	if err != nil {
		return 0, err
	}

	var n int64
	if err := json.Unmarshal(res, &n); err != nil {
		return 0, errors.New("invalid counter value")
	}

	if n == 1 {
		if _, err := l.command(ctx, "EXPIRE", k, int64(window/time.Second)); err != nil {
			// Never leave a counter behind that cannot expire.
			_, _ = l.command(ctx, "DEL", k)
			return 0, err
		}
	}
	return n, nil
}

// Count returns the current value of a counter without changing it (0 if the
// key does not exist).
func (l *Limiter) Count(ctx context.Context, key string) (int64, error) {
	res, err := l.command(ctx, "GET", keyPrefix+key)
	if err != nil {
		return 0, err
	}
	if string(res) == "null" {
		return 0, nil
	}

	// Redis returns counters as strings ("3"); accept a bare number too.
	var s string
	if err := json.Unmarshal(res, &s); err == nil {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, errors.New("invalid counter value")
		}
		return n, nil
	}
	var n int64
	if err := json.Unmarshal(res, &n); err != nil {
		return 0, errors.New("invalid counter value")
	}
	return n, nil
}

// RetryAfter returns how long until the counter for key resets (0 if unknown).
func (l *Limiter) RetryAfter(ctx context.Context, key string) time.Duration {
	res, err := l.command(ctx, "TTL", keyPrefix+key)
	if err != nil {
		return 0
	}
	var secs int64
	if err := json.Unmarshal(res, &secs); err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// Refund takes one event back off a counter (for example when the thing the
// event was counted for failed). It never lets the counter go below zero.
func (l *Limiter) Refund(ctx context.Context, key string) error {
	k := keyPrefix + key
	res, err := l.command(ctx, "DECR", k)
	if err != nil {
		return err
	}
	var n int64
	if err := json.Unmarshal(res, &n); err == nil && n < 0 {
		_, err = l.command(ctx, "SET", k, 0, "KEEPTTL")
		return err
	}
	return nil
}
