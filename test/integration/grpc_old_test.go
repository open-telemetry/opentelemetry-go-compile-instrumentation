// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestGRPCOld(t *testing.T) {
	pwd, err := os.Getwd()
	require.NoError(t, err)

	// Verifies that we can build a module with old gRPC version
	testutil.Build(t, filepath.Join(pwd, "..", "apps/grpcold"), "go", "build", "-a")
}
