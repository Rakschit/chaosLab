package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var ErrSessionNotFound = errors.New("session not found")

type Redis struct {
	url    string
	token  string
	client *http.Client
}

type redisResponse struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func NewRedis() (*Redis, error) {
	url := strings.TrimRight(
		strings.TrimSpace(os.Getenv("UPSTASH_REDIS_REST_URL")),
		"/",
	)
	token := strings.TrimSpace(
		os.Getenv("UPSTASH_REDIS_REST_TOKEN"),
	)

	if url == "" || token == "" {
		return nil, errors.New("Upstash Redis configuration is missing")
	}

	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("Upstash Redis URL must use HTTPS")
	}

	return &Redis{
		url:   url,
		token: token,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}, nil
}

func (r *Redis) command(
	ctx context.Context,
	args ...any,
) (json.RawMessage, error) {
	body, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		r.url,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Upstash request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(
		io.LimitReader(resp.Body, 1<<20),
	)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"Upstash returned HTTP %d",
			resp.StatusCode,
		)
	}

	var result redisResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("invalid Upstash response")
	}

	if result.Error != "" {
		return nil, fmt.Errorf("Upstash command failed: %s", result.Error)
	}

	return result.Result, nil
}

// Create or replace a session with a TTL.
func (r *Redis) Set(
	ctx context.Context,
	tokenHash string,
	userID string,
	ttl time.Duration,
) error {
	if tokenHash == "" || userID == "" {
		return errors.New("token hash and user ID are required")
	}
	if ttl < time.Second {
		return errors.New("session TTL must be at least one second")
	}

	seconds := int64(ttl / time.Second)

	_, err := r.command(
		ctx,
		"SET",
		"auth:session:"+tokenHash,
		userID,
		"EX",
		seconds,
	)
	return err
}

// Get returns the user ID associated with a session token hash.
func (r *Redis) Get(
	ctx context.Context,
	tokenHash string,
) (string, error) {
	if tokenHash == "" {
		return "", ErrSessionNotFound
	}

	result, err := r.command(
		ctx,
		"GET",
		"auth:session:"+tokenHash,
	)
	if err != nil {
		return "", err
	}

	if len(result) == 0 || string(result) == "null" {
		return "", ErrSessionNotFound
	}

	var userID string
	if err := json.Unmarshal(result, &userID); err != nil {
		return "", errors.New("invalid session data")
	}
	if userID == "" {
		return "", ErrSessionNotFound
	}

	return userID, nil
}

// Delete revokes a session.
func (r *Redis) Delete(
	ctx context.Context,
	tokenHash string,
) error {
	if tokenHash == "" {
		return errors.New("token hash is required")
	}

	_, err := r.command(
		ctx,
		"DEL",
		"auth:session:"+tokenHash,
	)
	return err
}
