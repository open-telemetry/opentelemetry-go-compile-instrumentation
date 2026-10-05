// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/instrumentation/database/sql/dsnparse"
)

// attrMap collapses the attribute slice into a lookup keyed by attribute name.
func attrMap(t *testing.T, req DatabaseSqlRequest) map[string]any {
	t.Helper()
	out := make(map[string]any)
	for _, attr := range DbClientRequestTraceAttrs(req) {
		out[string(attr.Key)] = attr.Value.AsInterface()
	}
	return out
}

// TestDbClientRequestTraceAttrs_IPv6Endpoint pins the behaviour of the
// SplitHostPort fallback for bracketed IPv6 endpoints.
//
// DbClientRequestTraceAttrs reports the whole endpoint as server.address and
// omits server.port whenever net.SplitHostPort fails, which makes a malformed
// endpoint silently degrade rather than error. These cases assert the
// bracketed form is understood so that degradation never kicks in for IPv6.
func TestDbClientRequestTraceAttrs_IPv6Endpoint(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		driverName string
		wantAddr   string
		wantPort   int64
	}{
		{
			name:       "ipv6 loopback",
			endpoint:   "[::1]:3306",
			driverName: "mysql",
			wantAddr:   "::1",
			wantPort:   3306,
		},
		{
			name:       "full ipv6",
			endpoint:   "[2001:db8::1]:5432",
			driverName: "postgres",
			wantAddr:   "2001:db8::1",
			wantPort:   5432,
		},
		{
			name:       "ipv6 with zone identifier",
			endpoint:   "[fe80::1%eth0]:9000",
			driverName: "clickhouse",
			wantAddr:   "fe80::1%eth0",
			wantPort:   9000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := attrMap(t, DatabaseSqlRequest{
				OpType:     "SELECT",
				Sql:        "SELECT 1",
				Endpoint:   tt.endpoint,
				DriverName: tt.driverName,
				DbName:     "mydb",
			})

			assert.Equal(t, tt.wantAddr, attrs["server.address"],
				"server.address must be the bare host, not the whole endpoint")
			require.Contains(t, attrs, "server.port",
				"server.port must be emitted for a bracketed IPv6 endpoint")
			assert.Equal(t, tt.wantPort, attrs["server.port"])
		})
	}
}

// TestDbClientRequestTraceAttrs_FromParsedIPv6DSN walks the full path a hook
// takes: parse the driver's DSN, compose the endpoint with Addr, then build the
// span attributes. It is the end-to-end guard for the dsnparse/semconv seam,
// where an unbracketed IPv6 endpoint used to cost both attributes at once.
func TestDbClientRequestTraceAttrs_FromParsedIPv6DSN(t *testing.T) {
	tests := []struct {
		name     string
		driver   string
		dsn      string
		wantAddr string
		wantPort int64
	}{
		{
			name:     "mysql",
			driver:   "mysql",
			dsn:      "user:pass@tcp([::1]:3306)/mydb",
			wantAddr: "::1",
			wantPort: 3306,
		},
		{
			name:     "postgres",
			driver:   "postgres",
			dsn:      "postgres://user:pass@[2001:db8::1]:5432/mydb",
			wantAddr: "2001:db8::1",
			wantPort: 5432,
		},
		{
			name:     "sqlserver",
			driver:   "sqlserver",
			dsn:      "sqlserver://[::1]:1433?database=mydb",
			wantAddr: "::1",
			wantPort: 1433,
		},
		{
			name:     "clickhouse",
			driver:   "clickhouse",
			dsn:      "clickhouse://[2001:db8::1]:9000/mydb",
			wantAddr: "2001:db8::1",
			wantPort: 9000,
		},
		{
			name:     "oracle",
			driver:   "godror",
			dsn:      "user/pass@//[::1]:1521/svc",
			wantAddr: "::1",
			wantPort: 1521,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := dsnparse.ParseDSN(tt.driver, tt.dsn)

			attrs := attrMap(t, DatabaseSqlRequest{
				OpType:     "SELECT",
				Sql:        "SELECT 1",
				Endpoint:   info.Addr(),
				DriverName: tt.driver,
				Dsn:        tt.dsn,
				DbName:     info.DBName,
			})

			assert.Equal(t, tt.wantAddr, attrs["server.address"])
			require.Contains(t, attrs, "server.port",
				"server.port dropped for endpoint %q parsed from %q", info.Addr(), tt.dsn)
			assert.Equal(t, tt.wantPort, attrs["server.port"])
		})
	}
}
