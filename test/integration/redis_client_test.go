// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

const redisGoRedisModule = "github.com/redis/go-redis/v9"

func TestRedisClient(t *testing.T) {
	t.Parallel()

	// Copy before either build. otelc setup mutates test/apps/redisclient/go.mod,
	// and a parallel copy would pick up the instrumentation require and MVS
	// to v9.22.0.
	legacyAppsDir := pinnedRedisClient(t, "v9.8.0")

	// Both cases go through otelc. Equal GET counts fail if the Conn version
	// gate is wrong: missing the pre-v9.9 hook drops a span, injecting the
	// hook after v9.9.0 adds a duplicate.
	for _, tc := range []struct {
		name    string
		appsDir string
		version string
	}{
		{name: "conn_after_v9.9"},
		{name: "conn_v9.8.0", appsDir: legacyAppsDir, version: "v9.8.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			testutil.Build(t, tc.appsDir, "redisclient", "go", "build", "-a")
			if tc.version != "" {
				requireRedisModuleVersion(t, filepath.Join(tc.appsDir, "redisclient"), tc.version)
			}

			assertRedisClientSpans(t, tc.appsDir)
			assertRedisRingSpans(t, tc.appsDir)
			assertRedisClusterSpans(t, tc.appsDir)
		})
	}
}

// assertRedisRingSpans checks that shards created by NewRing and Ring.SetAddrs
// get exactly one otel hook. Ring builds them with redis.NewClient, which is
// already instrumented.
func assertRedisRingSpans(t *testing.T, appsDir string) {
	t.Helper()

	f := testutil.NewTestFixture(t, fixtureOpts(appsDir)...)
	shard0 := StartRedisServer(t)
	shard1 := StartRedisServer(t)

	f.Run("redisclient", "-mode=ring", "-addr="+shard0.Addr(), "-addr2="+shard1.Addr())

	for _, server := range []*miniredis.Miniredis{shard0, shard1} {
		_, port, err := net.SplitHostPort(server.Addr())
		require.NoError(t, err)
		wantPort, err := strconv.Atoi(port)
		require.NoError(t, err)
		span := testutil.RequireSpan(t, f.Traces(),
			testutil.IsClient,
			testutil.HasAttribute("db.operation.name", "ping"),
			testutil.HasAttribute("server.port", int64(wantPort)),
		)
		testutil.RequireRedisClientSemconv(t, span, "ping", server.Addr(), "ping")
	}
	requireNoNestedRedisSpans(t, testutil.AllSpans(f.Traces()))
}

// assertRedisClusterSpans checks that lazily created cluster nodes get exactly
// one otel hook.
func assertRedisClusterSpans(t *testing.T, appsDir string) {
	t.Helper()

	f := testutil.NewTestFixture(t, fixtureOpts(appsDir)...)
	server := StartRedisServer(t)

	f.Run("redisclient", "-mode=cluster", "-addr="+server.Addr())

	span := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "get"),
	)
	testutil.RequireRedisClientSemconv(t, span, "get", server.Addr(), "get clusterkey")
	requireNoNestedRedisSpans(t, testutil.AllSpans(f.Traces()))
}

func fixtureOpts(appsDir string) []testutil.TestFixtureOption {
	if appsDir == "" {
		return nil
	}
	return []testutil.TestFixtureOption{testutil.WithAppsDir(appsDir)}
}

// requireNoNestedRedisSpans fails if a Redis client span is the child of a
// Redis client span with the same name, which happens when one client has two
// otel hooks. Other nesting is expected: go-redis runs its connection setup
// pipeline inside the first command.
func requireNoNestedRedisSpans(t *testing.T, spans []ptrace.Span) {
	t.Helper()

	redisSpans := make(map[pcommon.SpanID]ptrace.Span)
	for _, span := range spans {
		if testutil.IsClient(span) && testutil.HasAttribute("db.system.name", "redis")(span) {
			redisSpans[span.SpanID()] = span
		}
	}
	require.NotEmpty(t, redisSpans)
	for _, span := range redisSpans {
		parent, ok := redisSpans[span.ParentSpanID()]
		if ok && parent.Name() == span.Name() {
			require.Failf(
				t,
				"duplicate redis span",
				"redis span %q is nested in a redis span with the same name: the client has two otel hooks",
				span.Name(),
			)
		}
	}
}

func assertRedisClientSpans(t *testing.T, appsDir string) {
	t.Helper()

	f := testutil.NewTestFixture(t, fixtureOpts(appsDir)...)
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

	// Replace first so tidy cannot MVS up to the instrumentation module pin.
	goModOffWork(t, appDir, "edit", "-replace="+redisGoRedisModule+"="+redisGoRedisModule+"@"+version)
	cmd := exec.CommandContext(t.Context(), "go", "get", redisGoRedisModule+"@"+version)
	cmd.Dir = appDir
	cmd.Env = goWorkOffEnv()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go get %s@%s:\n%s", redisGoRedisModule, version, out)
	goModOffWork(t, appDir, "tidy")
	requireRedisModuleVersion(t, appDir, version)
	return appsDir
}

func requireRedisModuleVersion(t *testing.T, appDir, version string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Version}}", redisGoRedisModule)
	cmd.Dir = appDir
	cmd.Env = goWorkOffEnv()
	out, err := cmd.Output()
	require.NoError(t, err, "go list -m %s failed in %s", redisGoRedisModule, appDir)
	require.Equal(t, version, strings.TrimSpace(string(out)),
		"%s must stay at %s so the Conn version gate is the one under test", redisGoRedisModule, version)
}

func goModOffWork(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", append([]string{"mod"}, args...)...)
	cmd.Dir = dir
	cmd.Env = goWorkOffEnv()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func goWorkOffEnv() []string {
	return append(os.Environ(), "GOWORK=off")
}

// StartRedisServer creates and starts a miniredis server for testing.
// The server is automatically closed when the test completes.
func StartRedisServer(t *testing.T) *miniredis.Miniredis {
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}
