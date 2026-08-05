// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dsnparse

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The DSN parsers and the semconv attribute builders are coupled by a contract
// that neither side tested: the parsers compose an endpoint with DSNInfo.Addr,
// and semconv.DbClientRequestTraceAttrs takes it apart again with
// net.SplitHostPort to populate `server.address` and `server.port`.
//
// Per-dialect tests assert on the Host and Port fields, which were already
// correct for IPv6, so the corruption only appeared at the seam: Addr joined
// them with a bare colon, producing "::1:3306". net.SplitHostPort rejects that
// as "too many colons in address", and the semconv fallback then reports the
// whole endpoint as `server.address` and drops `server.port` entirely.
//
// The tests below pin the seam rather than either side of it.

// TestDSNInfo_AddrIPv6 covers the IPv6 cases missing from TestDSNInfo_Addr,
// which only exercised hostnames and IPv4 literals.
func TestDSNInfo_AddrIPv6(t *testing.T) {
	tests := []struct {
		name string
		info DSNInfo
		want string
	}{
		{
			name: "ipv6 loopback with port is bracketed",
			info: DSNInfo{Host: "::1", Port: "3306"},
			want: "[::1]:3306",
		},
		{
			name: "full ipv6 with port is bracketed",
			info: DSNInfo{Host: "2001:db8::1", Port: "5432"},
			want: "[2001:db8::1]:5432",
		},
		{
			name: "ipv6 zone identifier with port is bracketed",
			info: DSNInfo{Host: "fe80::1%eth0", Port: "1521"},
			want: "[fe80::1%eth0]:1521",
		},
		{
			// Without a port there is nothing to disambiguate, and semconv
			// falls back to using the endpoint verbatim as server.address,
			// so the bare host is what we want here.
			name: "ipv6 without port stays bare",
			info: DSNInfo{Host: "::1"},
			want: "::1",
		},
		{
			name: "ipv4 is unchanged",
			info: DSNInfo{Host: "10.0.0.1", Port: "5432"},
			want: "10.0.0.1:5432",
		},
		{
			name: "hostname is unchanged",
			info: DSNInfo{Host: "db.example.com", Port: "3306"},
			want: "db.example.com:3306",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.info.Addr())
		})
	}
}

// TestParseDSN_AddrRoundTripsThroughSplitHostPort is the regression test for
// the seam. For every dialect it parses an IPv6 DSN, composes the endpoint the
// way the instrumentation hook does, and requires that the semconv side can
// recover exactly the host and port the parser found.
func TestParseDSN_AddrRoundTripsThroughSplitHostPort(t *testing.T) {
	tests := []struct {
		name     string
		driver   string
		dsn      string
		wantHost string
		wantPort string
	}{
		{
			name:     "mysql parenthesised",
			driver:   "mysql",
			dsn:      "user:pass@tcp([::1]:3306)/mydb",
			wantHost: "::1",
			wantPort: "3306",
		},
		{
			name:     "mysql non-parenthesised",
			driver:   "mysql",
			dsn:      "user:pass@[2001:db8::1]:3306/mydb",
			wantHost: "2001:db8::1",
			wantPort: "3306",
		},
		{
			name:     "mysql default port",
			driver:   "mysql",
			dsn:      "user:pass@[fe80::1]/mydb",
			wantHost: "fe80::1",
			wantPort: "3306",
		},
		{
			name:     "postgres url",
			driver:   "postgres",
			dsn:      "postgres://user:pass@[::1]:5432/mydb",
			wantHost: "::1",
			wantPort: "5432",
		},
		{
			name:     "postgres libpq key=value",
			driver:   "postgres",
			dsn:      "host=::1 port=5432 dbname=mydb",
			wantHost: "::1",
			wantPort: "5432",
		},
		{
			name:     "postgres libpq bracketed host",
			driver:   "postgres",
			dsn:      "host=[2001:db8::1] port=5432 dbname=mydb",
			wantHost: "2001:db8::1",
			wantPort: "5432",
		},
		{
			name:     "sqlserver url",
			driver:   "sqlserver",
			dsn:      "sqlserver://[::1]:1433?database=mydb",
			wantHost: "::1",
			wantPort: "1433",
		},
		{
			name:     "sqlserver ado.net",
			driver:   "sqlserver",
			dsn:      "server=[2001:db8::1],1433;database=mydb",
			wantHost: "2001:db8::1",
			wantPort: "1433",
		},
		{
			name:     "clickhouse",
			driver:   "clickhouse",
			dsn:      "clickhouse://[2001:db8::1]:9000/mydb",
			wantHost: "2001:db8::1",
			wantPort: "9000",
		},
		{
			name:     "oracle url",
			driver:   "godror",
			dsn:      "oracle://user:pass@[::1]:1521/svc",
			wantHost: "::1",
			wantPort: "1521",
		},
		{
			name:     "oracle traditional notation",
			driver:   "godror",
			dsn:      "user/pass@//[::1]:1521/svc",
			wantHost: "::1",
			wantPort: "1521",
		},
		{
			name:     "oracle traditional notation default port",
			driver:   "godror",
			dsn:      "user/pass@//[2001:db8::1]/svc",
			wantHost: "2001:db8::1",
			wantPort: "1521",
		},
		{
			name:     "unregistered driver falls back to url parse",
			driver:   "somedriver",
			dsn:      "somedriver://[::1]:1234/mydb",
			wantHost: "::1",
			wantPort: "1234",
		},
		// IPv4 and hostnames must keep round-tripping unchanged.
		{
			name:     "mysql ipv4",
			driver:   "mysql",
			dsn:      "user:pass@tcp(127.0.0.1:3306)/mydb",
			wantHost: "127.0.0.1",
			wantPort: "3306",
		},
		{
			name:     "postgres hostname",
			driver:   "postgres",
			dsn:      "postgres://db.example.com:5432/mydb",
			wantHost: "db.example.com",
			wantPort: "5432",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := ParseDSN(tt.driver, tt.dsn)

			// The parser itself must expose the bare host, since that is what
			// server.address is meant to carry.
			assert.Equal(t, tt.wantHost, info.Host, "Host")
			assert.Equal(t, tt.wantPort, info.Port, "Port")

			addr := info.Addr()
			host, port, err := net.SplitHostPort(addr)
			require.NoErrorf(t, err, "endpoint %q is not splittable, so semconv would "+
				"report it verbatim as server.address and omit server.port", addr)
			assert.Equal(t, tt.wantHost, host, "server.address recovered from %q", addr)
			assert.Equal(t, tt.wantPort, port, "server.port recovered from %q", addr)
		})
	}
}

// TestDSNInfo_AddrNormalisesBracketedHost covers hosts that arrive still
// bracketed. DSNInfo is exported and RegisterDSNParser is a supported
// extension point, so a custom parser can hand back net/url's u.Host (which
// keeps the brackets) instead of u.Hostname(). Addr has to normalise rather
// than trust its input, or it double-brackets and breaks the same round trip.
func TestDSNInfo_AddrNormalisesBracketedHost(t *testing.T) {
	tests := []struct {
		name string
		info DSNInfo
		want string
	}{
		{
			name: "bracketed host with port",
			info: DSNInfo{Host: "[::1]", Port: "5432"},
			want: "[::1]:5432",
		},
		{
			name: "bracketed host without port",
			info: DSNInfo{Host: "[::1]"},
			want: "::1",
		},
		{
			name: "bracketed host with zone and port",
			info: DSNInfo{Host: "[fe80::1%eth0]", Port: "3306"},
			want: "[fe80::1%eth0]:3306",
		},
		{
			name: "empty brackets yield no address",
			info: DSNInfo{Host: "[]", Port: "5432"},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.info.Addr())
		})
	}
}

// TestRegisterDSNParser_BracketedHostRoundTrips drives the extension point end
// to end: a custom parser that returns a bracketed host must still produce an
// endpoint semconv can split.
func TestRegisterDSNParser_BracketedHostRoundTrips(t *testing.T) {
	RegisterDSNParser("bracketed-ipv6-driver", DSNParserFunc(func(string) DSNInfo {
		// Deliberately the u.Host shape, brackets included.
		return DSNInfo{Host: "[2001:db8::1]", Port: "5432", DBName: "mydb"}
	}))

	addr := ParseDSN("bracketed-ipv6-driver", "anything").Addr()
	host, port, err := net.SplitHostPort(addr)
	require.NoErrorf(t, err, "endpoint %q is not splittable", addr)
	assert.Equal(t, "2001:db8::1", host)
	assert.Equal(t, "5432", port)
}

// TestLegacyParseDSN_IPv6 checks the (addr, error) adapter used by
// beforeOpenInstrumentation returns the bracketed endpoint too, since that is
// the value stored on the instrumented *sql.DB and read back by every hook.
func TestLegacyParseDSN_IPv6(t *testing.T) {
	addr, err := LegacyParseDSN("mysql", "user:pass@tcp([::1]:3306)/mydb")
	require.NoError(t, err)
	assert.Equal(t, "[::1]:3306", addr)

	host, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	assert.Equal(t, "::1", host)
	assert.Equal(t, "3306", port)
}
