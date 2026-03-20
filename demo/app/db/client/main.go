// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"time"
)

const (
	defaultTimeout       = 10 * time.Second
	requestDelayDuration = 1 * time.Second
	startupWaitDuration  = 5 * time.Second
)

var (
	addr     = flag.String("addr", "http://localhost:8081", "The DB server address")
	count    = flag.Int("count", 10, "Number of CRUD cycles to run")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	logger   *slog.Logger
)

type User struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at,omitempty"`
}

func doRequest(ctx context.Context, client *http.Client, method, url string, body any) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewBuffer(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

func createUser(ctx context.Context, client *http.Client, baseURL string, user User) (*User, error) {
	body, status, err := doRequest(ctx, client, "POST", baseURL+"/user/create", user)
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated {
		return nil, fmt.Errorf("create user: status %d, body: %s", status, string(body))
	}
	var created User
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("unmarshal user: %w", err)
	}
	return &created, nil
}

func listUsers(ctx context.Context, client *http.Client, baseURL string) ([]User, error) {
	body, status, err := doRequest(ctx, client, "GET", baseURL+"/users", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("list users: status %d, body: %s", status, string(body))
	}
	var users []User
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, fmt.Errorf("unmarshal users: %w", err)
	}
	return users, nil
}

func updateUser(ctx context.Context, client *http.Client, baseURL string, user User) error {
	body, status, err := doRequest(ctx, client, "PUT", baseURL+"/user/update", user)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("update user: status %d, body: %s", status, string(body))
	}
	return nil
}

func deleteUser(ctx context.Context, client *http.Client, baseURL string, id int64) error {
	url := fmt.Sprintf("%s/user/delete?id=%d", baseURL, id)
	body, status, err := doRequest(ctx, client, "DELETE", url, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("delete user: status %d, body: %s", status, string(body))
	}
	return nil
}

func bulkCreate(ctx context.Context, client *http.Client, baseURL string, users []User) ([]User, error) {
	body, status, err := doRequest(ctx, client, "POST", baseURL+"/users/bulk", users)
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated {
		return nil, fmt.Errorf("bulk create: status %d, body: %s", status, string(body))
	}
	var created []User
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("unmarshal users: %w", err)
	}
	return created, nil
}

func runCRUDCycle(ctx context.Context, client *http.Client, baseURL string, cycle int) error {
	suffix := fmt.Sprintf("%d-%d", cycle, rand.IntN(10000))

	// Create a user
	user, err := createUser(ctx, client, baseURL, User{
		Name:  fmt.Sprintf("user-%s", suffix),
		Email: fmt.Sprintf("user-%s@example.com", suffix),
	})
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	logger.Info("created user", "cycle", cycle, "id", user.ID, "name", user.Name)

	time.Sleep(requestDelayDuration)

	// List all users
	users, err := listUsers(ctx, client, baseURL)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	logger.Info("listed users", "cycle", cycle, "count", len(users))

	time.Sleep(requestDelayDuration)

	// Update the user
	user.Name = fmt.Sprintf("updated-%s", suffix)
	user.Email = fmt.Sprintf("updated-%s@example.com", suffix)
	if err := updateUser(ctx, client, baseURL, *user); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	logger.Info("updated user", "cycle", cycle, "id", user.ID, "name", user.Name)

	time.Sleep(requestDelayDuration)

	// Bulk create (exercises transactions)
	bulkUsers := []User{
		{Name: fmt.Sprintf("bulk-a-%s", suffix), Email: fmt.Sprintf("bulk-a-%s@example.com", suffix)},
		{Name: fmt.Sprintf("bulk-b-%s", suffix), Email: fmt.Sprintf("bulk-b-%s@example.com", suffix)},
	}
	created, err := bulkCreate(ctx, client, baseURL, bulkUsers)
	if err != nil {
		return fmt.Errorf("bulk create: %w", err)
	}
	logger.Info("bulk created users", "cycle", cycle, "count", len(created))

	time.Sleep(requestDelayDuration)

	// Delete the originally created user
	if err := deleteUser(ctx, client, baseURL, user.ID); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	logger.Info("deleted user", "cycle", cycle, "id", user.ID)

	// Delete bulk-created users too
	for _, u := range created {
		if err := deleteUser(ctx, client, baseURL, u.ID); err != nil {
			return fmt.Errorf("delete bulk user: %w", err)
		}
	}
	logger.Info("deleted bulk users", "cycle", cycle)

	return nil
}

func main() {
	defer func() {
		time.Sleep(2 * time.Second)
	}()

	flag.Parse()

	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	client := &http.Client{Timeout: defaultTimeout}
	ctx := context.Background()

	logger.Info("client starting",
		"server_address", *addr,
		"cycle_count", *count,
		"log_level", *logLevel)

	// Wait for server to be ready
	logger.Info("waiting for server to be ready...")
	time.Sleep(startupWaitDuration)

	successCount := 0
	failureCount := 0

	for i := range *count {
		logger.Info("starting CRUD cycle", "cycle", i+1, "total", *count)

		if err := runCRUDCycle(ctx, client, *addr, i+1); err != nil {
			logger.Error("CRUD cycle failed", "cycle", i+1, "error", err)
			failureCount++
			continue
		}
		successCount++

		if i < *count-1 {
			time.Sleep(requestDelayDuration)
		}
	}

	logger.Info("client finished",
		"total_cycles", *count,
		"successful", successCount,
		"failed", failureCount)
}
