// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

const redisGoRedisModule = "github.com/redis/go-redis/v9"

func TestRedisClient(t *testing.T) {
	t.Parallel()

	// Both cases go through otelc. Equal GET counts fail if the Conn version
	// gate is wrong: missing the pre-v9.9 hook drops a span, injecting the
	// hook after v9.9.0 adds a duplicate.
	for _, tc := range []struct {
		name    string
		version string
	}{
		{name: "conn_after_v9.9", version: ""},
		{name: "conn_v9.8.0", version: "v9.8.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			appsDir := ""
			if tc.version != "" {
				appsDir = pinnedRedisClient(t, tc.version)
			}
			testutil.Build(t, appsDir, "redisclient", "go", "build", "-a")
			if tc.version != "" {
				requireRedisModuleVersion(t, filepath.Join(appsDir, "redisclient"), tc.version)
			}

			assertRedisClientSpans(t, appsDir)
		})
	}
}

func assertRedisClientSpans(t *testing.T, appsDir string) {
	t.Helper()

	var opts []testutil.TestFixtureOption
	if appsDir != "" {
		opts = append(opts, testutil.WithAppsDir(appsDir))
	}
	f := testutil.NewTestFixture(t, opts...)
	server := StartRedisServer(t)

	output := f.Run("redisclient", "-addr="+server.Addr())
	require.Contains(t, output, "testvalue")

	spans := testutil.AllSpans(f.Traces())
	require.GreaterOrEqual(t, len(spans), 4, "expected at least 4 spans (SET, GET, CONN GET, DEL)")

	setSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "set"),
	)
	testutil.RequireRedisClientSemconv(
		t,
		setSpan,
		"set",
		server.Addr(),
		"set testkey testvalue",
	)

	host, _, err := net.SplitHostPort(server.Addr())
	require.NoError(t, err)

	getCount := 0
	clientGet := false
	for _, span := range spans {
		if !testutil.IsClient(span) || !testutil.HasAttribute("db.operation.name", "get")(span) {
			continue
		}
		getCount++
		// Pre-v9.9 Conn hooks store Conn.String() as the endpoint, so only the
		// NewClient GET is guaranteed to have server.address == host.
		if testutil.HasAttribute("server.address", host)(span) {
			clientGet = true
			testutil.RequireRedisClientSemconv(
				t,
				span,
				"get",
				server.Addr(),
				"get testkey",
			)
		}
	}
	require.Equal(t, 2, getCount, "client GET and Conn GET should each produce one span")
	require.True(t, clientGet, "NewClient GET span is missing")

	delSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "del"),
	)
	testutil.RequireRedisClientSemconv(
		t,
		delSpan,
		"del",
		server.Addr(),
		"del testkey",
	)
}

// pinnedRedisClient copies redisclient and pins go-redis so otelc matches the
// Conn rule against that module version, not the committed go.mod.
func pinnedRedisClient(t *testing.T, version string) string {
	t.Helper()

	src, err := filepath.Abs(filepath.Join("..", "apps", "redisclient"))
	require.NoError(t, err)

	appsDir := t.TempDir()
	appDir := filepath.Join(appsDir, "redisclient")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	require.NoError(t, os.CopyFS(appDir, os.DirFS(src)))

	testutil.BumpToVersions(t, appDir, map[string]string{
		redisGoRedisModule: version,
	})
	// Instrumentation requires a newer go-redis; replace keeps the pin after setup.
	goMod(t, appDir, "edit", "-replace="+redisGoRedisModule+"="+redisGoRedisModule+"@"+version)
	goMod(t, appDir, "tidy")
	requireRedisModuleVersion(t, appDir, version)
	return appsDir
}

func requireRedisModuleVersion(t *testing.T, appDir, version string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Version}}", redisGoRedisModule)
	cmd.Dir = appDir
	out, err := cmd.Output()
	require.NoError(t, err, "go list -m %s failed in %s", redisGoRedisModule, appDir)
	require.Equal(t, version, strings.TrimSpace(string(out)),
		"%s must stay at %s so the Conn version gate is the one under test", redisGoRedisModule, version)
}

// StartRedisServer creates and starts a miniredis server for testing.
// The server is automatically closed when the test completes.
func StartRedisServer(t *testing.T) *miniredis.Miniredis {
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}
