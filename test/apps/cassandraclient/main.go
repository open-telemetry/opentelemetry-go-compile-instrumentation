// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal Cassandra client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"flag"
	"log"
	"log/slog"
	"time"

	"github.com/gocql/gocql"
)

var (
	addr     = flag.String("addr", "localhost:9042", "The Cassandra host address")
	keyspace = flag.String("keyspace", "testks", "The keyspace to use")
)

func main() {
	flag.Parse()

	// The bootstrap session runs against the default keyspace so it can create
	// the keyspace and table the traced session below operates on.
	bootstrap := gocql.NewCluster(*addr)
	bootstrap.Consistency = gocql.One
	bootstrap.Timeout = 30 * time.Second
	bootstrap.ConnectTimeout = 30 * time.Second

	admin, err := bootstrap.CreateSession()
	if err != nil {
		log.Fatalf("failed to create bootstrap session: %v", err)
	}

	err = admin.Query(`CREATE KEYSPACE IF NOT EXISTS ` + *keyspace +
		` WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}`).Exec()
	if err != nil {
		log.Fatalf("failed to create keyspace: %v", err)
	}

	err = admin.Query(`CREATE TABLE IF NOT EXISTS ` + *keyspace +
		`.users (id int PRIMARY KEY, name text)`).Exec()
	if err != nil {
		log.Fatalf("failed to create table: %v", err)
	}
	admin.Close()

	cluster := gocql.NewCluster(*addr)
	cluster.Keyspace = *keyspace
	cluster.Consistency = gocql.One
	cluster.Timeout = 30 * time.Second
	cluster.ConnectTimeout = 30 * time.Second

	session, err := cluster.CreateSession()
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}
	defer session.Close()

	err = session.Query(`INSERT INTO users (id, name) VALUES (?, ?)`, 1, "alice").Exec()
	if err != nil {
		log.Fatalf("failed to insert: %v", err)
	}
	slog.Info("INSERT", "id", 1, "name", "alice")

	var name string
	err = session.Query(`SELECT name FROM users WHERE id = ?`, 1).Scan(&name)
	if err != nil {
		log.Fatalf("failed to select: %v", err)
	}
	slog.Info("SELECT", "id", 1, "name", name)

	batch := session.NewBatch(gocql.LoggedBatch)
	batch.Query(`INSERT INTO users (id, name) VALUES (?, ?)`, 2, "bob")
	batch.Query(`INSERT INTO users (id, name) VALUES (?, ?)`, 3, "carol")
	if err := session.ExecuteBatch(batch); err != nil {
		log.Fatalf("failed to execute batch: %v", err)
	}
	slog.Info("BATCH", "statements", 2)

	slog.Info("Cassandra operations completed successfully")
}
