// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package early creates a gRPC client in a package-level variable. Its import
// path sorts before go.opentelemetry.io, so Go initializes it before the
// OpenTelemetry API, and the grpc.NewClient hook runs that early.
package early

import (
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Conn connects to the address given by the -addr= argument. Flags are not
// parsed yet during package initialization, so it reads os.Args directly.
var Conn = newConn()

func newConn() *grpc.ClientConn {
	addr := "localhost:50051"
	for _, arg := range os.Args[1:] {
		if v, ok := strings.CutPrefix(arg, "-addr="); ok {
			addr = v
		}
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	return conn
}
