// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestGRPCWithOtelgrpcStatsHandler runs the grpccontrib app with the otelgrpc
// stats handlers registered next to the otelc stats handler. grpc-go chains
// both handlers, so each RPC gets a span from each of them. Every exported
// span must have a parent that is exported too: otelc must end its own span
// instead of the innermost span on the context.
//
// The subtests are serial on purpose: they run the same app directory, and a
// testutil.Build writes one shared output path.
func TestGRPCWithOtelgrpcStatsHandler(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "grpccontrib", "go", "build", "-a")

	t.Run("server", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		port := testutil.FreePort(t)

		f.Start("grpccontrib", "-scenario=grpc-server", fmt.Sprintf("-front-port=%d", port))
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

		conn, err := grpc.NewClient(
			fmt.Sprintf("127.0.0.1:%d", port),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		require.NoError(t, err)
		defer conn.Close()

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
		require.NoError(t, err)

		// Two server spans per RPC: one from otelc and one from otelgrpc.
		f.WaitForSpans(2)
		f.RequireTraceCount(1)

		testutil.RequireNoDanglingParents(t, f.Traces())
	})

	t.Run("client", func(t *testing.T) {
		f := testutil.NewTestFixture(t)
		frontPort := testutil.FreePort(t)
		backPort := testutil.FreePort(t)

		f.Start("grpccontrib", "-scenario=grpc-client",
			fmt.Sprintf("-front-port=%d", frontPort),
			fmt.Sprintf("-back-port=%d", backPort))
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", frontPort))
		testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", backPort))

		req, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			fmt.Sprintf("http://127.0.0.1:%d/call", frontPort),
			nil,
		)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		// Front HTTP server span, two client spans (otelc and otelgrpc), and
		// the backend server span, all in one trace.
		f.WaitForSpans(4)
		f.RequireTraceCount(1)

		testutil.RequireNoDanglingParents(t, f.Traces())
	})
}
