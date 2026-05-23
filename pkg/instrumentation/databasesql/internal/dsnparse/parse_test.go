// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dsnparse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterDSNParser(t *testing.T) {
	const driver = "testdriver-register"

	// Not registered yet — should fall back to BestEffortParse.
	_, err := ParseDSN(driver, "not-a-url")
	assert.Error(t, err, "unregistered driver with unparseable DSN should return error")

	// Register a custom parser.
	called := false
	RegisterDSNParser(driver, DSNParserFunc(func(dsn string) (string, error) {
		called = true
		return "custom-host:1234", nil
	}))

	addr, err := ParseDSN(driver, "anything")
	require.NoError(t, err)
	assert.True(t, called, "registered parser should have been called")
	assert.Equal(t, "custom-host:1234", addr)
}

func TestRegisterDSNParser_Overwrite(t *testing.T) {
	const driver = "testdriver-overwrite"

	RegisterDSNParser(driver, DSNParserFunc(func(_ string) (string, error) { return "first:1111", nil }))
	RegisterDSNParser(driver, DSNParserFunc(func(_ string) (string, error) { return "second:2222", nil }))

	addr, err := ParseDSN(driver, "anything")
	require.NoError(t, err)
	assert.Equal(t, "second:2222", addr, "second registration should overwrite the first")
}

func TestBestEffortParse(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		want    string
		wantErr bool
	}{
		{
			name: "valid URL with host and port",
			dsn:  "somedriver://user:pass@db.example.com:9999/mydb",
			want: "db.example.com:9999",
		},
		{
			name: "valid URL with host only",
			dsn:  "somedriver://db.example.com/mydb",
			want: "db.example.com",
		},
		{
			name:    "no host in URL",
			dsn:     "not-a-url-at-all",
			wantErr: true,
		},
		{
			name:    "empty string",
			dsn:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BestEffortParse(tt.dsn)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestParseDSN_PgxAndLibPqAliases(t *testing.T) {
	const pgDSN = "postgres://user:pass@pg.example.com:5432/mydb"

	for _, driver := range []string{"pgx", "lib/pq"} {
		t.Run(driver, func(t *testing.T) {
			addr, err := ParseDSN(driver, pgDSN)
			require.NoError(t, err)
			assert.Equal(t, "pg.example.com:5432", addr, "driver %q should parse postgres DSN", driver)
		})
	}
}

func TestParseDSN_PostgresDefaultPort(t *testing.T) {
	// When no port is in the DSN the parser should append :5432.
	addr, err := ParseDSN("postgres", "postgres://user:pass@pg.example.com/mydb")
	require.NoError(t, err)
	assert.Equal(t, "pg.example.com:5432", addr)
}

func TestParseDSN_UnknownDriverFallback(t *testing.T) {
	// Unknown driver with a parseable URL should succeed via BestEffortParse.
	addr, err := ParseDSN("unknown-driver", "somedb://host.example.com:9876/mydb")
	require.NoError(t, err)
	assert.Equal(t, "host.example.com:9876", addr)

	// Unknown driver with an unparseable DSN should return an error.
	_, err = ParseDSN("unknown-driver", "not-a-url")
	assert.Error(t, err)
}

func TestParseDbName(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "postgres URL",
			dsn:  "postgres://user:pass@host:5432/mydb",
			want: "mydb",
		},
		{
			name: "mysql style",
			dsn:  "user:pass@tcp(host:3306)/mydb?charset=utf8",
			want: "mydb",
		},
		{
			name: "mysql style no params",
			dsn:  "user:pass@tcp(host:3306)/mydb",
			want: "mydb",
		},
		{
			name: "clickhouse URL",
			dsn:  "clickhouse://host:9000/analytics",
			want: "analytics",
		},
		{
			name: "unparseable DSN",
			dsn:  "not-a-dsn",
			want: "",
		},
		{
			name: "empty",
			dsn:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseDbName(tt.dsn))
		})
	}
}
