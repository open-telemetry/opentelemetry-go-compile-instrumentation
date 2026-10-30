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
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

const redisGoRedisModule = "github.com/redis/go-redis/v9"

func TestRedisClient(t *testing.T) {
	t.Parallel()

	// Copy before any build. otelc setup mutates test/apps/redisclient/go.mod,
	// and a parallel copy would pick up the instrumentation require and MVS
	// to v9.22.0.
	v980AppsDir := pinnedRedisClient(t, "v9.8.0")

	for _, tc := range []struct {
		name    string
		appsDir string
		version string
	}{
		{name: "current_supported"},
		{name: "v9.8.0", appsDir: v980AppsDir, version: "v9.8.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			testutil.Build(t, tc.appsDir, "redisclient", "go", "build", "-a")
			if tc.version != "" {
				requireRedisModuleVersion(t, filepath.Join(tc.appsDir, "redisclient"), tc.version)
			}

			assertRedisClientSpans(t, tc.appsDir)
			assertRedisSentinelSpans(t, tc.appsDir)
			assertRedisFailoverSpans(t, tc.appsDir)
			assertRedisRingSpans(t, tc.appsDir)
			assertRedisClusterSpans(t, tc.appsDir)
		})
	}
}

func assertRedisFailoverSpans(t *testing.T, appsDir string) {
	t.Helper()
	f := testutil.NewTestFixture(t, fixtureOpts(appsDir)...)
	first := StartRedisServer(t)
	second := StartRedisServer(t)
	sentinel := StartRedisServer(t)
	require.NoError(t, first.Set("before", "first"))
	require.NoError(t, second.Set("after", "second"))
	firstHost, firstPort, err := net.SplitHostPort(first.Addr())
	require.NoError(t, err)
	secondHost, secondPort, err := net.SplitHostPort(second.Addr())
	require.NoError(t, err)
	var switched atomic.Bool
	require.NoError(t, sentinel.Server().Register("SENTINEL", func(peer *server.Peer, _ string, args []string) {
		if len(args) == 0 {
			peer.WriteError("ERR missing subcommand")
			return
		}
		switch strings.ToLower(args[0]) {
		case "get-master-addr-by-name":
			host, port := firstHost, firstPort
			if switched.Load() {
				host, port = secondHost, secondPort
			}
			peer.WriteLen(2)
			peer.WriteBulk(host)
			peer.WriteBulk(port)
		case "sentinels":
			peer.WriteLen(0)
		default:
			peer.WriteError("ERR unknown sentinel subcommand")
		}
	}))
	require.NoError(t, sentinel.Server().Register("SWITCH", func(peer *server.Peer, _ string, _ []string) {
		switched.Store(true)
		first.Close()
		peer.WriteOK()
	}))
	output := f.Run("redisclient", "-mode=failover", "-addr="+sentinel.Addr())
	require.Contains(t, output, "first")
	require.Contains(t, output, "second")

	for _, query := range []string{"get before", "get after", "ping", "pipeline ping/"} {
		var matches []ptrace.Span
		for _, span := range testutil.AllSpans(f.Traces()) {
			if testutil.IsClient(span) && testutil.HasAttribute("db.query.text", query)(span) {
				matches = append(matches, span)
			}
		}
		require.Len(t, matches, 1, "expected one span for %s", query)
		attrs := testutil.Attrs(matches[0])
		require.NotContains(t, attrs, "server.address")
		require.NotContains(t, attrs, "server.port")
	}
}

func assertRedisSentinelSpans(t *testing.T, appsDir string) {
	t.Helper()
	f := testutil.NewTestFixture(t, fixtureOpts(appsDir)...)
	server := StartRedisServer(t)
	f.Run("redisclient", "-mode=sentinel", "-addr="+server.Addr())
	var pingSpans []ptrace.Span
	for _, span := range testutil.AllSpans(f.Traces()) {
		if testutil.IsClient(span) && testutil.HasAttribute("db.operation.name", "ping")(span) {
			pingSpans = append(pingSpans, span)
		}
	}
	require.Len(t, pingSpans, 1)
	testutil.RequireRedisClientSemconv(t, pingSpans[0], "ping", server.Addr(), "ping")
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

		var shardPingSpans []ptrace.Span
		for _, s := range testutil.AllSpans(f.Traces()) {
			if testutil.IsClient(s) &&
				testutil.HasAttribute("db.operation.name", "ping")(s) &&
				testutil.HasAttribute("server.port", int64(wantPort))(s) {
				shardPingSpans = append(shardPingSpans, s)
			}
		}
		require.Len(t, shardPingSpans, 1, "expected exactly one ping span for shard at port %d", wantPort)
		testutil.RequireRedisClientSemconv(t, shardPingSpans[0], "ping", server.Addr(), "ping")
	}
}

// assertRedisClusterSpans checks that lazily created cluster nodes get exactly
// one otel hook.
func assertRedisClusterSpans(t *testing.T, appsDir string) {
	t.Helper()

	f := testutil.NewTestFixture(t, fixtureOpts(appsDir)...)
	server := StartRedisServer(t)

	f.Run("redisclient", "-mode=cluster", "-addr="+server.Addr())

	var getSpans []ptrace.Span
	for _, s := range testutil.AllSpans(f.Traces()) {
		if testutil.IsClient(s) && testutil.HasAttribute("db.operation.name", "get")(s) {
			getSpans = append(getSpans, s)
		}
	}
	require.Len(t, getSpans, 1, "expected exactly one span for cluster command")
	testutil.RequireRedisClientSemconv(t, getSpans[0], "get", server.Addr(), "get clusterkey")
}

func fixtureOpts(appsDir string) []testutil.TestFixtureOption {
	if appsDir == "" {
		return nil
	}
	return []testutil.TestFixtureOption{testutil.WithAppsDir(appsDir)}
}

func assertRedisClientSpans(t *testing.T, appsDir string) {
	t.Helper()

	f := testutil.NewTestFixture(t, fixtureOpts(appsDir)...)
	server := StartRedisServer(t)

	output := f.Run("redisclient", "-addr="+server.Addr())
	require.Contains(t, output, "testvalue")

	var setSpans, getSpans, delSpans []ptrace.Span
	for _, span := range testutil.AllSpans(f.Traces()) {
		if !testutil.IsClient(span) {
			continue
		}
		attrs := testutil.Attrs(span)
		switch attrs["db.operation.name"] {
		case "set":
			setSpans = append(setSpans, span)
		case "get":
			getSpans = append(getSpans, span)
		case "del":
			delSpans = append(delSpans, span)
		}
	}

	require.Len(t, setSpans, 1, "expected exactly one SET span")
	testutil.RequireRedisClientSemconv(
		t,
		setSpans[0],
		"set",
		server.Addr(),
		"set testkey ?",
	)

	require.Len(t, getSpans, 2, "client GET and Conn GET should each produce one span")

	for _, query := range []string{"get testkey", "get connkey"} {
		var matched []ptrace.Span
		for _, span := range getSpans {
			if testutil.HasAttribute("db.query.text", query)(span) {
				matched = append(matched, span)
			}
		}
		require.Len(t, matched, 1, "expected one span for %s", query)
		testutil.RequireRedisClientSemconv(t, matched[0], "get", server.Addr(), query)
	}

	require.Len(t, delSpans, 1, "expected exactly one DEL span")
	testutil.RequireRedisClientSemconv(
		t,
		delSpans[0],
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
