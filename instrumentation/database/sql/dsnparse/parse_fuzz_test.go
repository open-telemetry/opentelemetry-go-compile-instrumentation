// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dsnparse

import (
	"net"
	"strings"
	"testing"
	"unicode/utf8"
)

// DSN strings arrive from application configuration and can be arbitrary: the
// hand-rolled dialect parsers slice them by byte offset, and a hook that
// panicked would take down the instrumented application rather than merely
// lose a span. ParseDSN documents "It never panics" and DSNParser requires the
// same of implementations, so the contract is worth asserting mechanically.
//
// These targets also run in ordinary `go test` runs, where the seed corpus
// executes as a normal table test, so they guard the listed cases at no extra
// CI cost. Run them as real fuzzers with, for example:
//
//	go test ./instrumentation/database/sql/dsnparse/ -run=Fuzz -fuzz=FuzzParseDSN -fuzztime=60s

// fuzzSeeds returns DSN strings covering every supported dialect plus the
// malformed shapes that historically broke offset arithmetic.
func fuzzSeeds() []string {
	return []string{
		// Well-formed, one per dialect.
		"postgres://user:pass@localhost:5432/mydb",
		"host='db.example.com' port=5432 dbname=prod user=alice",
		"user:pass@tcp(127.0.0.1:3306)/dbname?parseTime=true",
		"user:pass@unix(/var/run/mysqld/mysqld.sock)/dbname",
		"file:test.db?cache=shared&mode=memory",
		":memory:",
		"server=host,1433;database=mydb;user id=sa",
		"sqlserver://user:pass@host:1433?database=mydb",
		"clickhouse://host:9000/mydb",
		"user/pass@//host:1521/service",
		// IPv6 forms, which must survive the Addr round trip.
		"user:pass@tcp([::1]:3306)/mydb",
		"postgres://user:pass@[2001:db8::1]:5432/mydb",
		"user/pass@//[fe80::1%eth0]:1521/svc",
		// Malformed and boundary inputs.
		"",
		"://",
		"host=",
		"host='unterminated",
		"user:pass@tcp(unclosed/mydb",
		"[",
		"]",
		"[::1",
		"::1",
		"@",
		"/",
		"host=[::1] port=",
		strings.Repeat(":", 64),
	}
}

// fuzzDriverNames returns the registered driver names exercised by the fuzz
// targets, plus one unregistered name to reach the best-effort fallback.
func fuzzDriverNames() []string {
	return []string{
		"postgres", "pgx", "mysql", "mariadb", "sqlite3",
		"sqlserver", "mssql", "clickhouse", "godror", "oracle",
		"definitely-not-registered",
	}
}

// checkAddrRoundTrip asserts the endpoint composed by Addr can be taken apart
// again by net.SplitHostPort, which is how the semconv helpers derive
// server.address and server.port.
//
// The property is only meaningful for a well-formed result. A DSN can name a
// host containing stray brackets or a non-numeric port, and for those Addr is
// allowed to produce something unsplittable: semconv then degrades gracefully
// by reporting the endpoint verbatim. Demanding a round trip there would
// assert on garbage rather than on the contract that matters.
func checkAddrRoundTrip(t *testing.T, driver, dsn string, info DSNInfo) {
	t.Helper()

	if info.Host == "" || info.Port == "" {
		return
	}
	if strings.ContainsAny(info.Host, "[]") || !isAllDigits(info.Port) {
		return
	}

	addr := info.Addr()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("driver=%q dsn=%q: Addr()=%q is not splittable (%v); semconv would "+
			"report the whole endpoint as server.address and drop server.port",
			driver, dsn, addr, err)
	}
	if host != info.Host || port != info.Port {
		t.Fatalf("driver=%q dsn=%q: Addr()=%q round-tripped to (%q, %q), want (%q, %q)",
			driver, dsn, addr, host, port, info.Host, info.Port)
	}
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// FuzzParseDSN asserts that parsing never panics for any driver and that a
// well-formed result composes an endpoint the semconv layer can split again.
func FuzzParseDSN(f *testing.F) {
	for _, seed := range fuzzSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, dsn string) {
		for _, driver := range fuzzDriverNames() {
			info := ParseDSN(driver, dsn)
			checkAddrRoundTrip(t, driver, dsn, info)

			// LegacyParseDSN is the adapter the db hook actually calls; it must
			// always yield a non-empty endpoint, falling back to the driver
			// name when the DSN carries no address.
			addr, err := LegacyParseDSN(driver, dsn)
			if err != nil {
				t.Fatalf("driver=%q dsn=%q: LegacyParseDSN returned error %v", driver, dsn, err)
			}
			if addr == "" {
				t.Fatalf("driver=%q dsn=%q: LegacyParseDSN returned an empty endpoint", driver, dsn)
			}
		}
	})
}

// FuzzParseMySQLDSNPassword asserts that a password containing any DSN
// delimiter ('@', '/', '(', '?') never leaks into Host, Port, or DBName.
func FuzzParseMySQLDSNPassword(f *testing.F) {
	for _, password := range []string{
		"", "password", "pa(ss", "s3cr/et?x", "p@secret/hidden?tail",
	} {
		f.Add(password)
	}

	templates := []struct {
		address string
		want    DSNInfo
	}{
		{"tcp(db.example.com:3306)", DSNInfo{Host: "db.example.com", Port: "3306", DBName: "app"}},
		{"unix(/tmp/mysql.sock)", DSNInfo{Host: "/tmp/mysql.sock", DBName: "app"}},
		{"db.example.com:3306", DSNInfo{Host: "db.example.com", Port: "3306", DBName: "app"}},
	}

	f.Fuzz(func(t *testing.T, password string) {
		for _, tmpl := range templates {
			dsn := "user:" + password + "@" + tmpl.address + "/app"
			if got := ParseDSN("mysql", dsn); got != tmpl.want {
				t.Fatalf("dsn=%q: ParseDSN() = %#v, want %#v", dsn, got, tmpl.want)
			}
		}
	})
}

// FuzzParseOracleDSNPassword asserts that no part of a quoted Oracle password
// reaches the parsed address, whatever characters the password contains.
func FuzzParseOracleDSNPassword(f *testing.F) {
	for _, password := range []string{
		"", "password", "p@ss", "a@b@c", "s3cr/et?x", "p@secret//hidden:1521",
	} {
		f.Add(password)
	}

	f.Fuzz(func(t *testing.T, password string) {
		if strings.Contains(password, `"`) {
			t.Skip("a quoted Oracle password cannot contain a double quote")
		}
		if strings.Contains(password, "\n") || strings.Contains(password, "connectString=") {
			t.Skip("godror reads such a DSN as logfmt")
		}
		dsn := `user/"` + password + `"@dbhost:1521/ORCL`
		got := ParseDSN("godror", dsn)
		want := DSNInfo{Host: "dbhost", Port: "1521", DBName: "ORCL"}
		if got != want {
			t.Fatalf("password=%q: ParseDSN() = %#v, want %#v", password, got, want)
		}
	})
}

// FuzzParseOracleDSNUnquotedPassword asserts the same for an unquoted
// password, which can carry an '@' either escaped as \@ (godror) or as is
// (go-oci8 splits on the last '@').
func FuzzParseOracleDSNUnquotedPassword(f *testing.F) {
	for _, password := range []string{
		"password", `p\@ss`, `a\@b\@c`, "p@ss", "a@b@c", "s3cr/et?x", `p\@secret//hidden:1521`,
	} {
		f.Add(password)
	}

	f.Fuzz(func(t *testing.T, password string) {
		if strings.Contains(password, `"`) {
			t.Skip("a double quote would start a quoted password")
		}
		if strings.Contains(password, "\n") || strings.Contains(password, "connectString=") {
			t.Skip("godror reads such a DSN as logfmt")
		}
		dsn := `user/` + password + `@dbhost:1521/ORCL`
		got := ParseDSN("godror", dsn)
		want := DSNInfo{Host: "dbhost", Port: "1521", DBName: "ORCL"}
		if got != want {
			t.Fatalf("password=%q: ParseDSN() = %#v, want %#v", password, got, want)
		}
	})
}

// FuzzParseOracleDSNLogfmtPassword asserts that no part of a password reaches
// the address of a logfmt DSN written the way godror's StringWithPassword
// writes it, with and without a new password, and with the connect string in
// either position.
func FuzzParseOracleDSNLogfmtPassword(f *testing.F) {
	for _, password := range []string{
		"", "password", "ti@ger", "a@b@c", `a"b@c"d`, `a\b@c`, "p@ss word", "x connectString=evil:1/e", "s3cr/et?x",
	} {
		f.Add(password)
	}

	f.Fuzz(func(t *testing.T, password string) {
		if !utf8.ValidString(password) {
			t.Skip("logfmt values are valid UTF-8")
		}
		for _, r := range password {
			if r < ' ' {
				t.Skip("control characters are escaped by the writer")
			}
		}
		value := password
		if strings.ContainsAny(value, " =\"\\") {
			value = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
		}
		want := DSNInfo{Host: "dbhost", Port: "1521", DBName: "ORCL"}
		for _, dsn := range []string{
			"user=scott password=" + value + " connectString=dbhost:1521/ORCL",
			"connectString=dbhost:1521/ORCL user=scott password=" + value,
			"user=scott password=" + value + " connectString=dbhost:1521/ORCL connectionClass= newPassword=" + value,
			"user=scott password=" + value + " newPassword=" + value + " connectString=dbhost:1521/ORCL connectionClass=",
		} {
			if got := ParseDSN("godror", dsn); got != want {
				t.Fatalf("dsn=%q: ParseDSN() = %#v, want %#v", dsn, got, want)
			}
		}
	})
}

// FuzzParseOracleURLPassword asserts that no part of a raw or percent-encoded
// password reaches the address of an oracle:// DSN. go-oci8 splits on the last
// '@' without decoding anything.
func FuzzParseOracleURLPassword(f *testing.F) {
	for _, password := range []string{
		"password", "p@ss", "a@b@c", "2024/Spring", "p@ss/w", "p%40ss", "s3cr:et", "p?x", "p@secret//hidden:1521",
	} {
		f.Add(password)
	}

	f.Fuzz(func(t *testing.T, password string) {
		if strings.Contains(password, "\n") || strings.Contains(password, "connectString=") {
			t.Skip("godror reads such a DSN as logfmt")
		}
		if strings.Contains(password, "?") {
			// A '?' in the password makes the credentials/query boundary ambiguous,
			// so the parser safely yields no address rather than risk leaking password bytes.
			if got := ParseDSN("oci8", "oracle://scott:"+password+"@dbhost:1521/ORCL"); got != (DSNInfo{}) {
				t.Fatalf("password=%q: ParseDSN() = %#v, want zero value", password, got)
			}
			return
		}
		dsn := "oracle://scott:" + password + "@dbhost:1521/ORCL"
		got := ParseDSN("oci8", dsn)
		want := DSNInfo{Host: "dbhost", Port: "1521", DBName: "ORCL"}
		if got != want {
			t.Fatalf("password=%q: ParseDSN() = %#v, want %#v", password, got, want)
		}
	})
}

// FuzzParseDbName asserts the backward-compatible database-name extractor
// never panics and never returns a value containing its own delimiters, which
// would mean a query string leaked into db.namespace.
func FuzzParseDbName(f *testing.F) {
	for _, seed := range fuzzSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, dsn string) {
		name := ParseDbName(dsn)
		// PathUnescape can legitimately turn %2F into '/', so only the
		// query-string delimiters are checked here.
		if strings.ContainsAny(name, "?&") {
			t.Fatalf("dsn=%q: ParseDbName returned %q, which still contains a query-string delimiter",
				dsn, name)
		}
	})
}
