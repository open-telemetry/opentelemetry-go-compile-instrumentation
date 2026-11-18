// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package early creates MongoDB clients in package-level variables. Its import
// path sorts before go.opentelemetry.io, so Go initializes it before the
// OpenTelemetry API, and the mongo.Connect hooks run that early.
package early

import (
	"context"
	"os"
	"strings"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	mongov2 "go.mongodb.org/mongo-driver/v2/mongo"
	optionsv2 "go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	// ClientV1 uses mongo-driver v1.
	ClientV1 = must(mongo.Connect(context.Background(), options.Client().ApplyURI(uri())))
	// ClientV2 uses mongo-driver v2.
	ClientV2 = must(mongov2.Connect(optionsv2.Client().ApplyURI(uri())))
)

// uri reads the -uri= argument. Flags are not parsed yet during package
// initialization, so it reads os.Args directly.
func uri() string {
	for _, arg := range os.Args[1:] {
		if v, ok := strings.CutPrefix(arg, "-uri="); ok {
			return v
		}
	}
	return "mongodb://localhost:27017"
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
