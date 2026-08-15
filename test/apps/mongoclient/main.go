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
	bsonv2 "go.mongodb.org/mongo-driver/v2/bson"
	mongov2 "go.mongodb.org/mongo-driver/v2/mongo"
	optionsv2 "go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	version = flag.String("version", "2", "MongoDB driver version")
	uri     = flag.String("uri", "mongodb://localhost:27017", "MongoDB connection URI")
)

func main() {
	flag.Parse()

	ctx := context.Background()

	slog.Info("Connecting to MongoDB", "uri", *uri, "version", *version)

	switch *version {
	case "1":
		v1(ctx)
	case "2":
		v2(ctx)
	default:
		log.Fatalf("invalid version: %v", *version)
	}

	slog.Info("MongoDB operations completed successfully")
}

func v1(ctx context.Context) {
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
}

func v2(ctx context.Context) {
	client, err := mongov2.Connect(optionsv2.Client().ApplyURI(*uri))
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
	_, err = collection.InsertOne(ctx, bsonv2.D{
		{Key: "name", Value: "LFX Mentee"},
		{Key: "status", Value: "active"},
	})
	if err != nil {
		log.Fatalf("failed to insert document: %v", err)
	}
}
