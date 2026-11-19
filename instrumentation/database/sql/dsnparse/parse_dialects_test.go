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
			name:     "easy connect with tcps protocol prefix",
			dsn:      "user/pass@tcps://dbhost:2484/ORCL",
			wantHost: "dbhost",
			wantPort: "2484",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect with tcp protocol prefix",
			dsn:      "user/pass@tcp://dbhost:1521/ORCL",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect protocol prefix is case insensitive",
			dsn:      "user/pass@TCPS://dbhost:2484/ORCL",
			wantHost: "dbhost",
			wantPort: "2484",
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
		{
			name:     "easy connect with parameters",
			dsn:      "user/pass@dbhost:1521/ORCL?connect_timeout=5&retry_count=3",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect with server type and instance name",
			dsn:      "user/pass@//dbhost:1521/ORCL:dedicated/inst1",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect with parameters and no service name",
			dsn:      "user/pass@dbhost:1521?connect_timeout=5",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "",
		},
		{
			name:     "url with server type and instance name",
			dsn:      "oracle://user:pass@dbhost:1521/ORCL:pooled/inst1?x=1",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect with bracketed ipv6 and parameters",
			dsn:      "user/pass@[::1]:1521/ORCL?x=1",
			wantHost: "::1",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect quoted password with at sign",
			dsn:      `user/"p@ss"@dbhost:1521/ORCL`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect quoted password with several at signs",
			dsn:      `user/"a@b@c"@//dbhost:1521/ORCL`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect backslash escaped at sign in password",
			dsn:      `user/p\@ss@dbhost:1521/ORCL`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect unescaped at signs in password",
			dsn:      `user/a@b@c@dbhost:1521/ORCL`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect quoted user name with at sign",
			dsn:      `"us@er"/pass@dbhost:1521/ORCL`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "easy connect doubled quote inside quoted password",
			dsn:      `user/"pa""s@s"@dbhost:1521/ORCL`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "logfmt password with at sign",
			dsn:      `user=scott password=s3cr3t@word connectString=dbhost:1521/ORCL`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "logfmt connect string first",
			dsn:      `connectString=dbhost:1521/ORCL user=scott password=s3cr3t@word`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "logfmt with new password",
			dsn:      `user=scott password=ti@ger connectString=dbhost:1521/ORCL connectionClass= newPassword=N3wSecret@x standaloneConnection=0`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "logfmt new password after connect string",
			dsn:      "user=scott password=ti@ger connectString=dbhost:1521/ORCL\nconnectionClass= newPassword=N3wSecret",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "logfmt escaped quote in password",
			dsn:      `user=scott password="a\"b@c\"d" connectString=dbhost:1521/ORCL`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "logfmt quoted connect string",
			dsn:      `user=scott password=p@ss connectString="//dbhost:1521/ORCL"`,
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
		},
		{
			name:     "logfmt tcps connect string",
			dsn:      `user=scott password=p@ss connectString=tcps://dbhost:2484/ORCL?retry_count=3`,
			wantHost: "dbhost",
			wantPort: "2484",
			wantDB:   "ORCL",
		},
		{
			name:     "logfmt separated by newline",
			dsn:      "user=scott\npassword=p@ss\nconnectString=dbhost:1521/ORCL",
			wantHost: "dbhost",
			wantPort: "1521",
			wantDB:   "ORCL",
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

// TestOracleStripProtocol pins that the "//" separator and only a leading known
// transport keyword are removed: a "//" later in the DSN belongs to the service
// name or a parameter, not to the address.
func TestOracleStripProtocol(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tcps prefix", "tcps://dbhost:2484/ORCL", "dbhost:2484/ORCL"},
		{"uppercase prefix", "TCPS://dbhost/ORCL", "dbhost/ORCL"},
		{"unknown protocol", "foo://dbhost/ORCL", "foo://dbhost/ORCL"},
		{"bare double slash", "//dbhost:1521/ORCL", "dbhost:1521/ORCL"},
		{"protocol later in the string", "dbhost:1521/ORCL?retry=://x", "dbhost:1521/ORCL?retry=://x"},
		{"separator later in the string", "dbhost:1521/ORCL?retry=//x", "dbhost:1521/ORCL?retry=//x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, oracleStripProtocol(tt.in))
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

// TestParseDSN_OracleUnclosedQuote checks that a password whose opening quote is
// never closed yields no address, rather than an address built from the part
// of the password after an '@'.
func TestParseDSN_OracleUnclosedQuote(t *testing.T) {
	got := ParseDSN("godror", `user/"p@ss@dbhost:1521/ORCL`)
	assert.Equal(t, DSNInfo{}, got)
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

// TestParseDSN_MySQLUnixSocketNonParenthesized covers the "unix:" form without
// parentheses. The address itself is an absolute path and contains '/', so the
// database separator has to be the last '/' in the DSN, not the first.
func TestParseDSN_MySQLUnixSocketNonParenthesized(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantHost string
		wantPort string
		wantDB   string
	}{
		{
			name:     "unix socket with dbname",
			dsn:      "unix:/tmp/mysql.sock/db",
			wantHost: "/tmp/mysql.sock",
			wantPort: "",
			wantDB:   "db",
		},
		{
			name:     "unix socket with credentials and dbname",
			dsn:      "user:pass@unix:/tmp/mysql.sock/mydb",
			wantHost: "/tmp/mysql.sock",
			wantPort: "",
			wantDB:   "mydb",
		},
		{
			name:     "unix socket with trailing slash and no dbname",
			dsn:      "unix:/tmp/mysql.sock/",
			wantHost: "/tmp/mysql.sock",
			wantPort: "",
			wantDB:   "",
		},
		{
			name:     "unix socket nested under multiple directories",
			dsn:      "unix:/var/run/mysqld/mysqld.sock/prod",
			wantHost: "/var/run/mysqld/mysqld.sock",
			wantPort: "",
			wantDB:   "prod",
		},
		{
			// Parenthesized form is unaffected by the non-parenthesized fix; kept
			// here as a same-file regression check alongside the cases above.
			name:     "parenthesized form unaffected",
			dsn:      "unix(/tmp/mysql.sock)/db",
			wantHost: "/tmp/mysql.sock",
			wantPort: "",
			wantDB:   "db",
		},
		{
			// A protocol keyword that merely starts with "unix" is not the unix
			// transport; only an exact "unix:" prefix switches the separator.
			name:     "protocol name starting with unix but not exact",
			dsn:      "unixish:3306/db",
			wantHost: "unixish",
			wantPort: "3306",
			wantDB:   "db",
		},
		{
			name:     "tcp form unaffected by unix handling",
			dsn:      "user:pass@tcp:3306/mydb",
			wantHost: "localhost",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "bare host form unaffected by unix handling",
			dsn:      "user:pass@host:3306/mydb",
			wantHost: "host",
			wantPort: "3306",
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

// TestParseDSN_OracleLogfmtNoAddress checks that a logfmt DSN with no usable
// connectString reports no address, whatever else it holds.
func TestParseDSN_OracleLogfmtNoAddress(t *testing.T) {
	for name, dsn := range map[string]string{
		"no connect string":     "user=scott\npassword=p@ss:1521/secret",
		"unclosed quoted value": `user=scott password="p@ss connectString=dbhost:1521/ORCL`,
		"unclosed quoted host":  `connectString="dbhost:1521/ORCL user=scott password=p@ss`,
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, DSNInfo{}, ParseDSN("godror", dsn))
		})
	}
}

// TestParseDSN_OracleURL covers oracle:// DSNs. go-oci8 splits on the last '@'
// after the scheme, so a raw '@', '/' or ':' in the password must not reach
// the address, while the percent-encoded form used by go-ora and godror keeps
// its result.
func TestParseDSN_OracleURL(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantHost string
		wantPort string
		wantDB   string
	}{
		{"password with slash and colon", "oracle://scott:2024/Spring@dbhost:1521/ORCL", "dbhost", "1521", "ORCL"},
		{"password with at sign and slash", "oracle://scott:p@ss/w@dbhost:1521/ORCL", "dbhost", "1521", "ORCL"},
		{"password with several at signs", "oracle://scott:a@b@c@dbhost:1521/ORCL", "dbhost", "1521", "ORCL"},
		{"percent encoded password", "oracle://scott:p%40ss%2Fw@dbhost:1521/ORCL", "dbhost", "1521", "ORCL"},
		{"percent encoded password default port", "oracle://scott:p%40ss@dbhost/ORCL", "dbhost", "1521", "ORCL"},
		{"ipv6 host", "oracle://scott:p%40ss@[::1]:1522/ORCL", "::1", "1522", "ORCL"},
		{"upper case scheme", "ORACLE://scott:p@ss@dbhost:1521/ORCL", "dbhost", "1521", "ORCL"},
		{"query without at sign", "oracle://scott:tiger@dbhost:1521/ORCL?retry_count=3", "dbhost", "1521", "ORCL"},
		{"no credentials", "oracle://dbhost:1521/ORCL", "dbhost", "1521", "ORCL"},
		{"no credentials default port", "oracle://dbhost/ORCL", "dbhost", "1521", "ORCL"},
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

// TestParseDSN_OracleURLAmbiguous checks that oracle:// DSNs with ambiguous
// boundaries between query and credentials yield no address rather than leaking
// password bytes. This includes passwords containing '?' (which could be the
// start of a query) and queries containing '@' (which could be a credentials
// separator in a raw URL).
func TestParseDSN_OracleURLAmbiguous(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
	}{
		{"query with at sign", "oracle://scott:tiger@dbhost:1521/ORCL?wallet=a@b"},
		{"password with question mark", "oracle://scott:p?x@dbhost:1521/ORCL"},
		{"slash user and password with question mark", "oracle://scott/pa?ss@dbhost:1521/ORCL"},
		{"port-like user and password with question mark", "oracle://scott:12/b?c@dbhost:1521/ORCL"},
		{"at sign and question mark in password", "oracle://scott/p@ss?x@dbhost:1521/ORCL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, driver := range []string{"oracle", "godror", "oci8", "go-oci8"} {
				got := ParseDSN(driver, tt.dsn)
				assert.Equal(t, DSNInfo{}, got, "driver=%s", driver)
			}
		})
	}
}

// TestParseDSN_OracleEasyConnectAtAfterSeparator pins a known limitation: an
// '@' that comes after the credentials separator, here in the Easy Connect
// Plus ssl_server_cert_dn parameter, is taken for the separator. The host is
// then wrong, which is accepted because the other reading can disclose part of
// a password.
func TestParseDSN_OracleEasyConnectAtAfterSeparator(t *testing.T) {
	got := ParseDSN("godror", "scott/tiger@tcps://dbhost:2484/ORCL?ssl_server_cert_dn=CN=db,EMAIL=dba@example.com")
	assert.Equal(t, DSNInfo{Host: "example.com", Port: "1521", DBName: ""}, got)
}
