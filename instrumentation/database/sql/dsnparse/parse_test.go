// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dsnparse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseDSN_Postgres(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantHost string
		wantPort string
		wantDB   string
	}{
		// ---- URL format ----
		{
			name:     "url with credentials and port",
			dsn:      "postgres://user:pass@localhost:5432/mydb",
			wantHost: "localhost",
			wantPort: "5432",
			wantDB:   "mydb",
		},
		{
			name:     "url no credentials",
			dsn:      "postgres://localhost:5432/mydb",
			wantHost: "localhost",
			wantPort: "5432",
			wantDB:   "mydb",
		},
		{
			name:     "url no port uses default 5432",
			dsn:      "postgres://localhost/mydb",
			wantHost: "localhost",
			wantPort: "5432",
			wantDB:   "mydb",
		},
		{
			name:     "postgresql scheme",
			dsn:      "postgresql://user:pass@db.example.com:5433/prod",
			wantHost: "db.example.com",
			wantPort: "5433",
			wantDB:   "prod",
		},
		{
			name:     "url with query params",
			dsn:      "postgres://user:pass@host:5432/mydb?sslmode=require&connect_timeout=5",
			wantHost: "host",
			wantPort: "5432",
			wantDB:   "mydb",
		},
		{
			name:     "url with ip address",
			dsn:      "postgres://10.0.0.1:5432/mydb",
			wantHost: "10.0.0.1",
			wantPort: "5432",
			wantDB:   "mydb",
		},
		{
			name:     "url with encoded password",
			dsn:      "postgres://user:p%40ss@host:5432/mydb",
			wantHost: "host",
			wantPort: "5432",
			wantDB:   "mydb",
		},
		{
			name:     "url empty dbname",
			dsn:      "postgres://host:5432/",
			wantHost: "host",
			wantPort: "5432",
			wantDB:   "",
		},
		// ---- Libpq key=value format (the previously failing case) ----
		{
			name:     "libpq all fields",
			dsn:      "host=localhost port=5432 dbname=mydb",
			wantHost: "localhost",
			wantPort: "5432",
			wantDB:   "mydb",
		},
		{
			name:     "libpq no port uses default",
			dsn:      "host=db.example.com dbname=prod",
			wantHost: "db.example.com",
			wantPort: "5432",
			wantDB:   "prod",
		},
		{
			name:     "libpq with extra fields",
			dsn:      "host=localhost port=5432 dbname=mydb user=alice password=secret sslmode=disable",
			wantHost: "localhost",
			wantPort: "5432",
			wantDB:   "mydb",
		},
		{
			name:     "libpq single-quoted host",
			dsn:      "host='db.example.com' port=5432 dbname=prod",
			wantHost: "db.example.com",
			wantPort: "5432",
			wantDB:   "prod",
		},
		{
			name:     "libpq only dbname uses default host and port",
			dsn:      "dbname=mydb",
			wantHost: "localhost",
			wantPort: "5432",
			wantDB:   "mydb",
		},
		{
			name:     "libpq custom port",
			dsn:      "host=replica.internal port=5433 dbname=analytics",
			wantHost: "replica.internal",
			wantPort: "5433",
			wantDB:   "analytics",
		},
		{
			name:     "libpq single-quoted dbname with space",
			dsn:      "host=localhost port=5432 dbname='my db'",
			wantHost: "localhost",
			wantPort: "5432",
			wantDB:   "my db",
		},
		// pgx uses the same DSN format
		{
			name:     "pgx driver url format",
			dsn:      "postgres://user:pass@host:5432/mydb",
			wantHost: "host",
			wantPort: "5432",
			wantDB:   "mydb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, driver := range []string{"postgres", "pgx", "postgresql"} {
				got := ParseDSN(driver, tt.dsn)
				assert.Equal(t, tt.wantHost, got.Host, "driver=%s Host", driver)
				assert.Equal(t, tt.wantPort, got.Port, "driver=%s Port", driver)
				assert.Equal(t, tt.wantDB, got.DBName, "driver=%s DBName", driver)
			}
		})
	}
}

func TestParseDSN_MySQL(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantHost string
		wantPort string
		wantDB   string
	}{
		// ---- Standard go-sql-driver/mysql format (parenthesised address) ----
		{
			name:     "full credentials tcp",
			dsn:      "user:pass@tcp(localhost:3306)/mydb",
			wantHost: "localhost",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "no password",
			dsn:      "user@tcp(localhost:3306)/mydb",
			wantHost: "localhost",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "no credentials",
			dsn:      "tcp(localhost:3306)/mydb",
			wantHost: "localhost",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "ip address",
			dsn:      "user:pass@tcp(127.0.0.1:3306)/mydb",
			wantHost: "127.0.0.1",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "with query params",
			dsn:      "user:pass@tcp(localhost:3306)/mydb?charset=utf8&timeout=5s",
			wantHost: "localhost",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "remote host",
			dsn:      "user:pass@tcp(db.example.com:3306)/prod",
			wantHost: "db.example.com",
			wantPort: "3306",
			wantDB:   "prod",
		},
		{
			name:     "tcp no port",
			dsn:      "user:pass@tcp(localhost)/mydb",
			wantHost: "localhost",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "password with @ symbol uses last @",
			dsn:      "user:p@ss@tcp(host:3306)/mydb",
			wantHost: "host",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "unix socket",
			dsn:      "user:pass@unix(/tmp/mysql.sock)/mydb",
			wantHost: "/tmp/mysql.sock",
			wantPort: "",
			wantDB:   "mydb",
		},
		{
			name:     "just dbname via slash",
			dsn:      "/mydb",
			wantHost: "",
			wantPort: "",
			wantDB:   "mydb",
		},
		{
			name:     "credentials and dbname only",
			dsn:      "user:pass@/mydb",
			wantHost: "",
			wantPort: "",
			wantDB:   "mydb",
		},
		// ---- Non-standard format without parentheses (previously failing case) ----
		{
			name:     "tcp protocol with bare port",
			dsn:      "user:pass@tcp:3306/mydb",
			wantHost: "localhost",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "no protocol host:port",
			dsn:      "user:pass@localhost:3306/mydb",
			wantHost: "localhost",
			wantPort: "3306",
			wantDB:   "mydb",
		},
		{
			name:     "ip host:port no protocol",
			dsn:      "user:pass@10.0.0.5:3306/proddb",
			wantHost: "10.0.0.5",
			wantPort: "3306",
			wantDB:   "proddb",
		},
		{
			name:     "empty db after slash",
			dsn:      "user:pass@tcp(host:3306)/",
			wantHost: "host",
			wantPort: "3306",
			wantDB:   "",
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

func TestParseDSN_SQLite(t *testing.T) {
	tests := []struct {
		name   string
		dsn    string
		wantDB string
	}{
		// ---- file: URI format (the previously failing case) ----
		{
			name:   "file URI with query params",
			dsn:    "file:test.db?cache=shared",
			wantDB: "test.db",
		},
		{
			name:   "file URI no params",
			dsn:    "file:test.db",
			wantDB: "test.db",
		},
		{
			name:   "file URI absolute path",
			dsn:    "file:/var/lib/myapp/data.db",
			wantDB: "data.db",
		},
		{
			name:   "file URI relative path with dirs",
			dsn:    "file:../data/test.db?mode=ro",
			wantDB: "test.db",
		},
		{
			name:   "file URI in-memory",
			dsn:    "file::memory:?cache=shared",
			wantDB: ":memory:",
		},
		{
			name:   "file URI double-slash in-memory",
			dsn:    "file://:memory:",
			wantDB: ":memory:",
		},
		// ---- :memory: shorthand ----
		{
			name:   "in-memory shorthand",
			dsn:    ":memory:",
			wantDB: ":memory:",
		},
		// ---- bare filename ----
		{
			name:   "bare filename",
			dsn:    "test.db",
			wantDB: "test.db",
		},
		{
			name:   "bare filename with query",
			dsn:    "test.db?cache=shared",
			wantDB: "test.db",
		},
		{
			name:   "bare path",
			dsn:    "/var/lib/data.db",
			wantDB: "data.db",
		},
		{
			name:   "production db name",
			dsn:    "file:production.sqlite3?mode=ro&cache=shared",
			wantDB: "production.sqlite3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, driver := range []string{"sqlite3", "sqlite"} {
				got := ParseDSN(driver, tt.dsn)
				assert.Equal(t, "sqlite3", got.Host, "driver=%s Host should always be sqlite3", driver)
				assert.Equal(t, tt.wantDB, got.DBName, "driver=%s DBName", driver)
			}
		})
	}
}

func TestParseDSN_SQLServer(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantHost string
		wantPort string
		wantDB   string
	}{
		// ---- URL format ----
		{
			name:     "sqlserver url",
			dsn:      "sqlserver://user:pass@host:1433?database=mydb",
			wantHost: "host",
			wantPort: "1433",
			wantDB:   "mydb",
		},
		{
			name:     "mssql url",
			dsn:      "mssql://user:pass@host:1433?database=mydb",
			wantHost: "host",
			wantPort: "1433",
			wantDB:   "mydb",
		},
		{
			name:     "sqlserver url default port",
			dsn:      "sqlserver://user:pass@db.example.com?database=prod",
			wantHost: "db.example.com",
			wantPort: "1433",
			wantDB:   "prod",
		},
		{
			name:     "sqlserver url ip",
			dsn:      "sqlserver://sa:secret@10.0.0.1:1433?database=sales",
			wantHost: "10.0.0.1",
			wantPort: "1433",
			wantDB:   "sales",
		},
		// ---- ADO.NET semicolon key=value format ----
		{
			name:     "ado.net lowercase keys",
			dsn:      "server=host;port=1433;database=mydb;user id=sa;password=secret",
			wantHost: "host",
			wantPort: "1433",
			wantDB:   "mydb",
		},
		{
			name:     "ado.net mixed case keys",
			dsn:      "Server=db.example.com;Database=prod;User Id=sa;Password=secret",
			wantHost: "db.example.com",
			wantPort: "1433",
			wantDB:   "prod",
		},
		{
			name:     "ado.net server with comma port",
			dsn:      "Server=host,1434;Database=mydb;User Id=sa;Password=secret",
			wantHost: "host",
			wantPort: "1434",
			wantDB:   "mydb",
		},
		{
			name:     "ado.net initial catalog key",
			dsn:      "server=host;initial catalog=mydb;user id=sa",
			wantHost: "host",
			wantPort: "1433",
			wantDB:   "mydb",
		},
		{
			name:     "ado.net data source key",
			dsn:      "data source=db.example.com;initial catalog=sales;user id=sa;password=p",
			wantHost: "db.example.com",
			wantPort: "1433",
			wantDB:   "sales",
		},
		{
			name:     "ado.net server with backslash instance",
			dsn:      "Server=host\\SQLEXPRESS;Database=mydb;User Id=sa;Password=p",
			wantHost: "host",
			wantPort: "1433",
			wantDB:   "mydb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, driver := range []string{"sqlserver", "mssql"} {
				got := ParseDSN(driver, tt.dsn)
				assert.Equal(t, tt.wantHost, got.Host, "driver=%s Host", driver)
				assert.Equal(t, tt.wantPort, got.Port, "driver=%s Port", driver)
				assert.Equal(t, tt.wantDB, got.DBName, "driver=%s DBName", driver)
			}
		})
	}
}

func TestParseDSN_UnknownDriver(t *testing.T) {
	got := ParseDSN("someunknowndriver", "host=localhost")
	assert.Equal(t, DSNInfo{}, got)
	assert.Equal(t, "", got.Addr())
}

func TestDSNInfo_Addr(t *testing.T) {
	tests := []struct {
		info DSNInfo
		want string
	}{
		{DSNInfo{Host: "localhost", Port: "5432"}, "localhost:5432"},
		{DSNInfo{Host: "localhost", Port: ""}, "localhost"},
		{DSNInfo{Host: "", Port: "5432"}, ""},
		{DSNInfo{}, ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.info.Addr())
	}
}

func TestParseDbName(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "standard url dbname",
			dsn:  "postgres://user:pass@host:5432/mydb",
			want: "mydb",
		},
		{
			name: "mysql standard",
			dsn:  "user:pass@tcp(host:3306)/mydb",
			want: "mydb",
		},
		{
			name: "with query string",
			dsn:  "user:pass@tcp(host:3306)/mydb?charset=utf8",
			want: "mydb",
		},
		{
			name: "url encoded dbname",
			dsn:  "postgres://host/my%20db",
			want: "my db",
		},
		{
			name: "no slash returns empty",
			dsn:  "host=localhost dbname=mydb",
			want: "",
		},
		{
			name: "sqlite bare filename",
			dsn:  "file:test.db?cache=shared",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseDbName(tt.dsn))
		})
	}
}

func TestParseDSN_SQLitePreviouslyFailing(t *testing.T) {
	// Regression test: prior to this fix, file:test.db?cache=shared returned
	// DBName="" because ParseDbName found no '/' in the string.
	got := ParseDSN("sqlite3", "file:test.db?cache=shared")
	assert.Equal(t, "test.db", got.DBName, "DBName must be the filename, not empty")
	assert.Equal(t, "sqlite3", got.Host)
}

func TestParseDSN_PostgresLibpqPreviouslyFailing(t *testing.T) {
	// Regression test: prior to this fix, the libpq KV format fell through the
	// URL check (wrong scheme) and returned addr="unknown" / dbname="".
	got := ParseDSN("postgres", "host=localhost port=5432 dbname=mydb")
	assert.Equal(t, "localhost", got.Host)
	assert.Equal(t, "5432", got.Port)
	assert.Equal(t, "mydb", got.DBName)
}

func TestParseDSN_MySQLNoParensPreviouslyFailing(t *testing.T) {
	// Regression test: prior to this fix, user:pass@tcp:3306/dbname returned
	// an error because the parser required parentheses around the address.
	got := ParseDSN("mysql", "user:pass@tcp:3306/mydb")
	assert.Equal(t, "localhost", got.Host)
	assert.Equal(t, "3306", got.Port)
	assert.Equal(t, "mydb", got.DBName)
}
