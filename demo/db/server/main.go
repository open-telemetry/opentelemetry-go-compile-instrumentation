// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const (
	maxRetries    = 30
	retryInterval = 2 * time.Second
)

var (
	port     = flag.Int("port", 8081, "The server port")
	dsn      = flag.String("dsn", "demo:demo@tcp(mysql:3306)/demodb?parseTime=true", "MySQL DSN")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	logger   *slog.Logger
)

type User struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("failed to encode response", "error", err)
	}
}

func waitForMySQL(ctx context.Context, db *sql.DB) error {
	for i := range maxRetries {
		if err := db.PingContext(ctx); err == nil {
			logger.Info("mysql connection established", "attempt", i+1)
			return nil
		}
		logger.Warn("waiting for mysql...", "attempt", i+1, "max_retries", maxRetries)
		time.Sleep(retryInterval)
	}
	return fmt.Errorf("mysql not available after %d retries", maxRetries)
}

func initSchema(ctx context.Context, db *sql.DB) error {
	query := `CREATE TABLE IF NOT EXISTS users (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		email VARCHAR(255) NOT NULL UNIQUE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`
	_, err := db.ExecContext(ctx, query)
	return err
}

func makeHandleCreateUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var user User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			logger.Error("failed to decode request", "error", err)
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		result, err := db.ExecContext(r.Context(),
			"INSERT INTO users (name, email) VALUES (?, ?)", user.Name, user.Email)
		if err != nil {
			logger.Error("failed to insert user", "error", err, "name", user.Name)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to create user"})
			return
		}

		id, _ := result.LastInsertId()
		user.ID = id
		logger.Info("user created", "id", id, "name", user.Name, "email", user.Email)
		writeJSON(w, http.StatusCreated, user)
	}
}

func makeHandleListUsers(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		name := r.URL.Query().Get("name")
		var rows *sql.Rows
		var err error
		if name != "" {
			rows, err = db.QueryContext(r.Context(),
				"SELECT id, name, email, created_at FROM users WHERE name = ?", name)
		} else {
			rows, err = db.QueryContext(r.Context(),
				"SELECT id, name, email, created_at FROM users")
		}
		if err != nil {
			logger.Error("failed to query users", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to list users"})
			return
		}
		defer rows.Close()

		var users []User
		for rows.Next() {
			var u User
			if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt); err != nil {
				logger.Error("failed to scan user", "error", err)
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to scan user"})
				return
			}
			users = append(users, u)
		}
		if err := rows.Err(); err != nil {
			logger.Error("rows iteration error", "error", err)
		}

		logger.Info("listed users", "count", len(users), "filter_name", name)
		writeJSON(w, http.StatusOK, users)
	}
}

func makeHandleGetUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		idStr := r.URL.Query().Get("id")
		if idStr == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "missing id parameter"})
			return
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
			return
		}

		var u User
		err = db.QueryRowContext(r.Context(),
			"SELECT id, name, email, created_at FROM users WHERE id = ?", id).
			Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "user not found"})
			return
		}
		if err != nil {
			logger.Error("failed to get user", "error", err, "id", id)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to get user"})
			return
		}

		logger.Info("fetched user", "id", u.ID, "name", u.Name)
		writeJSON(w, http.StatusOK, u)
	}
}

func makeHandleUpdateUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var user User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			logger.Error("failed to decode request", "error", err)
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}
		if user.ID == 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "missing user id"})
			return
		}

		result, err := db.ExecContext(r.Context(),
			"UPDATE users SET name = ?, email = ? WHERE id = ?", user.Name, user.Email, user.ID)
		if err != nil {
			logger.Error("failed to update user", "error", err, "id", user.ID)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to update user"})
			return
		}

		rows, _ := result.RowsAffected()
		if rows == 0 {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "user not found"})
			return
		}

		logger.Info("user updated", "id", user.ID, "name", user.Name, "email", user.Email)
		writeJSON(w, http.StatusOK, user)
	}
}

func makeHandleDeleteUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		idStr := r.URL.Query().Get("id")
		if idStr == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "missing id parameter"})
			return
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
			return
		}

		result, err := db.ExecContext(r.Context(),
			"DELETE FROM users WHERE id = ?", id)
		if err != nil {
			logger.Error("failed to delete user", "error", err, "id", id)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to delete user"})
			return
		}

		rows, _ := result.RowsAffected()
		if rows == 0 {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "user not found"})
			return
		}

		logger.Info("user deleted", "id", id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// handleBulkCreate demonstrates transaction usage: insert multiple users atomically.
func makeHandleBulkCreate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var users []User
		if err := json.NewDecoder(r.Body).Decode(&users); err != nil {
			logger.Error("failed to decode request", "error", err)
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			logger.Error("failed to begin transaction", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to start transaction"})
			return
		}

		stmt, err := tx.PrepareContext(r.Context(), "INSERT INTO users (name, email) VALUES (?, ?)")
		if err != nil {
			tx.Rollback()
			logger.Error("failed to prepare statement", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to prepare statement"})
			return
		}
		defer stmt.Close()

		for i := range users {
			result, err := stmt.ExecContext(r.Context(), users[i].Name, users[i].Email)
			if err != nil {
				tx.Rollback()
				logger.Error("failed to insert user in transaction", "error", err, "name", users[i].Name)
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to insert user: " + err.Error()})
				return
			}
			id, _ := result.LastInsertId()
			users[i].ID = id
		}

		if err := tx.Commit(); err != nil {
			logger.Error("failed to commit transaction", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to commit transaction"})
			return
		}

		logger.Info("bulk insert committed", "count", len(users))
		writeJSON(w, http.StatusCreated, users)
	}
}

func makeHandleHealth(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			logger.Error("health check failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy", "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func main() {
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

	logger.Info("connecting to mysql", "dsn", *dsn)

	db, err := sql.Open("mysql", *dsn)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx := context.Background()
	if err := waitForMySQL(ctx, db); err != nil {
		logger.Error("mysql not available", "error", err)
		os.Exit(1)
	}

	if err := initSchema(ctx, db); err != nil {
		logger.Error("failed to initialize schema", "error", err)
		os.Exit(1)
	}
	logger.Info("database schema initialized")

	http.HandleFunc("/health", makeHandleHealth(db))
	http.HandleFunc("/users", makeHandleListUsers(db))
	http.HandleFunc("/user", makeHandleGetUser(db))
	http.HandleFunc("/user/create", makeHandleCreateUser(db))
	http.HandleFunc("/user/update", makeHandleUpdateUser(db))
	http.HandleFunc("/user/delete", makeHandleDeleteUser(db))
	http.HandleFunc("/users/bulk", makeHandleBulkCreate(db))

	addr := fmt.Sprintf(":%d", *port)
	logger.Info("server starting",
		"address", addr,
		"log_level", *logLevel)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("failed to listen", "error", err)
		os.Exit(1)
	}
	defer listener.Close()

	logger.Info("server started", "address", listener.Addr())
	if err := http.Serve(listener, nil); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
