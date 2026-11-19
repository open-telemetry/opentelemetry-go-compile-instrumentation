// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main inserts a document through each MongoDB client created during
// package initialization.
package main

import (
	"context"
	"flag"
	"log"

	"go.mongodb.org/mongo-driver/bson"
	bsonv2 "go.mongodb.org/mongo-driver/v2/bson"

	"example.com/mongoinit/early"
)

func main() {
	flag.String("uri", "", "The MongoDB URI, read by package early")
	flag.Parse()
	ctx := context.Background()

	if _, err := early.ClientV1.Database("testdb").Collection("users_v1").
		InsertOne(ctx, bson.D{{Key: "name", Value: "v1"}}); err != nil {
		log.Fatalf("v1 insert failed: %v", err)
	}
	if _, err := early.ClientV2.Database("testdb").Collection("users_v2").
		InsertOne(ctx, bsonv2.D{{Key: "name", Value: "v2"}}); err != nil {
		log.Fatalf("v2 insert failed: %v", err)
	}
	if err := early.ClientV1.Disconnect(ctx); err != nil {
		log.Fatalf("v1 disconnect failed: %v", err)
	}
	if err := early.ClientV2.Disconnect(ctx); err != nil {
		log.Fatalf("v2 disconnect failed: %v", err)
	}
}
