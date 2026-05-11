// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is the multi-instrumentation benchmark scenario.
//
// It references functions that otelc has instrumentation rules for across all
// supported libraries so the full AST rewriting path is exercised during
// compilation. The binary is never executed — only compile time is measured.
package main

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	_ = http.NewServeMux()
	_ = &http.Transport{}

	srv := grpc.NewServer()
	_ = srv
	conn, _ := grpc.NewClient("passthrough:///localhost", grpc.WithTransportCredentials(insecure.NewCredentials()))
	_ = conn

	db, _ := sql.Open("noop", "")
	_ = db

	rdb := redis.NewClient(&redis.Options{})
	_ = rdb

	_ = context.Background()
}
