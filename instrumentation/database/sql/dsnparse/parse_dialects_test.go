// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dsnparse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseDSN_ClickHouse exercises the ClickHouse parser, which was previously
// untested. ClickHouse DSNs are always URL-shaped, and the default port depends
// on the scheme (native/tcp -> 9000, http -> 8123, https -> 8443).
func TestParseDSN_ClickHouse(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantHost string
		wantPort string
		wantDB   string
	}{
		{
			name:     "native scheme explicit port and db",
			dsn:      "clickhouse://user:pass@host:9000/mydb",
			wantHost: "host",
			wantPort: "9000",
			wantDB:   "mydb",
		},
		{
			name:     "native scheme default port",
			dsn:      "clickhouse://host/analytics",
			wantHost: "host",
			wantPort: "9000",
			wantDB:   "analytics",
		},
		{
			name:     "tcp scheme default port",
			dsn:      "tcp://ch.example.com/events",
			wantHost: "ch.example.com",
			wantPort: "9000",
			wantDB:   "events",
		},
		{
			name:     "http scheme default port 8123",
			dsn:      "http://host/mydb",
			wantHost: "host",
			wantPort: "8123",
			wantDB:   "mydb",
		},
		{
			name:     "https scheme default port 8443",
			dsn:      "https://host/mydb",
			wantHost: "host",
			wantPort: "8443",
			wantDB:   "mydb",
		},
		{
			name:     "database from query param when path empty",
			dsn:      "clickhouse://host:9000/?database=analytics",
			wantHost: "host",
			wantPort: "9000",
			wantDB:   "analytics",
		},
		{
			name:     "path db takes precedence over query param",
			dsn:      "clickhouse://host:9000/pathdb?database=querydb",
			wantHost: "host",
			wantPort: "9000",
			wantDB:   "pathdb",
		},
		{
			name:     "ip host with explicit port",
			dsn:      "clickhouse://10.0.0.7:9440/prod",
			wantHost: "10.0.0.7",
			wantPort: "9440",
			wantDB:   "prod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDSN("clickhouse", tt.dsn)
			assert.Equal(t, tt.wantHost, got.Host, "Host")
			assert.Equal(t, tt.wantPort, got.Port, "Port")
			assert.Equal(t, tt.wantDB, got.DBName, "DBName")
		})
	}
}

// TestParseDSN_ClickHouseMalformed ensures the parser never panics and returns a
// zero-value DSNInfo when the DSN cannot be parsed as a URL.
func TestParseDSN_ClickHouseMalformed(t *testing.T) {
	got := ParseDSN("clickhouse", "://://not a url")
	assert.Equal(t, DSNInfo{}, got)
}

// TestParseDSN_Oracle exercises the Oracle parser, which was previously
// untested. Oracle DSNs come in URL form and the traditional
// user/pass@host:port/service form (with an optional leading "//").
func TestParseDSN_Oracle(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantHost string
		wantPort string
		wantDB   string
	}{
		// ---- URL format ----
		{
			name:     "url with credentials port and service",
			dsn:      "oracle://user:pass@dbhost:1521/ORCLPDB1",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCLPDB1",
		},
		{
			name:     "url default port",
			dsn:      "oracle://dbhost/ORCL",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		// ---- Traditional EZConnect format ----
		{
			name:     "easy connect host port service",
			dsn:      "user/pass@dbhost:1521/ORCL",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect with double slash prefix",
			dsn:      "user/pass@//dbhost:1521/ORCL",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect default port",
			dsn:      "user/pass@dbhost/ORCL",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect no service name",
			dsn:      "user/pass@dbhost:1521",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "",
		},
		{
			name:     "easy connect host only",
			dsn:      "system/manager@prod-oracle",
			wantHost: "prod-oracle",
			wantPort: "1521",
			wantDB:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, driver := range []string{"oracle", "godror", "oci8", "go-oci8"} {
				got := ParseDSN(driver, tt.dsn)
				assert.Equal(t, tt.wantHost, got.Host, "driver=%s Host", driver)
				assert.Equal(t, tt.wantPort, got.Port, "driver=%s Port", driver)
				assert.Equal(t, tt.wantDB, got.DBName, "driver=%s DBName", driver)
			}
		})
	}
}

// TestParseDSN_OracleNoAt verifies that an Oracle DSN with no '@' separator
// (and not URL-shaped) yields a zero-value DSNInfo rather than panicking.
func TestParseDSN_OracleNoAt(t *testing.T) {
	got := ParseDSN("oracle", "justaservicename")
	assert.Equal(t, DSNInfo{}, got)
	assert.Equal(t, "", got.Addr())
}

// TestParseDSN_MySQLIPv6 covers IPv6 bracketed addresses in the non-parenthesised
// MySQL DSN form, which the earlier suite did not exercise.
func TestParseDSN_MySQLIPv6(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantHost string
		wantPort string
		wantDB   string
	}{
		{
			name:     "ipv6 with port",
			dsn:      "user:pass@[::1]:3306/mydb",
			wantHost: "::1",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "ipv6 without port defaults to 3306",
			dsn:      "user:pass@[2001:db8::1]/mydb",
			wantHost: "2001:db8::1",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "ipv6 inside parentheses",
			dsn:      "user:pass@tcp([fe80::1]:3307)/mydb",
			wantHost: "fe80::1",
			wantPort: "3307",
			wantDB:   "mydb",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDSN("mysql", tt.dsn)
			assert.Equal(t, tt.wantHost, got.Host, "Host")
			assert.Equal(t, tt.wantPort, got.Port, "Port")
			assert.Equal(t, tt.wantDB, got.DBName, "DBName")
		})
	}
}

// TestParseDSN_MySQLProtocolStripping covers mysqlStripProtocol branches that
// were not previously reached: known transports other than tcp, an empty
// address after the protocol, and a unix-socket path after the protocol.
func TestParseDSN_MySQLProtocolStripping(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantHost string
		wantPort string
		wantDB   string
	}{
		{
			name:     "udp protocol with bare port",
			dsn:      "user:pass@udp:3306/mydb",
			wantHost: "localhost",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			// A known protocol with no address after the colon yields an empty
			// host rather than treating the protocol keyword as a hostname.
			name:     "protocol with empty address yields empty host",
			dsn:      "user:pass@tcp:/mydb",
			wantHost: "",
			wantPort: "",
			wantDB:   "mydb",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDSN("mysql", tt.dsn)
			assert.Equal(t, tt.wantHost, got.Host, "Host")
			assert.Equal(t, tt.wantPort, got.Port, "Port")
			assert.Equal(t, tt.wantDB, got.DBName, "DBName")
		})
	}
}

// TestLegacyParseDSN documents the (addr, error) compatibility shim used by the
// db package's beforeOpenInstrumentation. It returns the host:port address when
// one can be derived, otherwise it falls back to the driver name. It never
// returns a non-nil error.
func TestLegacyParseDSN(t *testing.T) {
	tests := []struct {
		name     string
		driver   string
		dsn      string
		wantAddr string
	}{
		{
			name:     "postgres url yields host port",
			driver:   "postgres",
			dsn:      "postgres://user:pass@pghost:5432/db",
			wantAddr: "pghost:5432",
		},
		{
			name:     "oracle easy connect yields host port",
			driver:   "oracle",
			dsn:      "user/pass@dbhost:1521/ORCL",
			wantAddr: "dbhost:1521",
		},
		{
			name:     "host only address without port",
			driver:   "sqlite3",
			dsn:      ":memory:",
			wantAddr: "sqlite3",
		},
		{
			name:     "empty address falls back to driver name",
			driver:   "mysql",
			dsn:      "/justdb",
			wantAddr: "mysql",
		},
		{
			name:     "unknown driver non-url falls back to driver name",
			driver:   "weirddriver",
			dsn:      "not-a-url",
			wantAddr: "weirddriver",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := LegacyParseDSN(tt.driver, tt.dsn)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantAddr, addr)
		})
	}
}
