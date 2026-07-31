// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal MongoDB client for integration testing.
// This client is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var uri = flag.String("uri", "mongodb://localhost:27017", "MongoDB connection URI")

func main() {
	flag.Parse()

	ctx := context.Background()

	slog.Info("Connecting to MongoDB", "uri", *uri)
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(*uri))
	if err != nil {
		log.Fatalf("failed to connect to MongoDB: %v", err)
	}
	defer func() {
		if err := client.Disconnect(ctx); err != nil {
			log.Fatalf("failed to disconnect from MongoDB: %v", err)
		}
	}()

	collection := client.Database("testdb").Collection("users")

	slog.Info("Inserting document into MongoDB")
	_, err = collection.InsertOne(ctx, bson.D{
		{Key: "name", Value: "LFX Mentee"},
		{Key: "status", Value: "active"},
	})
	if err != nil {
		log.Fatalf("failed to insert document: %v", err)
	}

	slog.Info("MongoDB operations completed successfully")
}
