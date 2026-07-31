// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCRC32(t *testing.T) {
	// CRC32 is deterministic and returns a decimal string.
	got := CRC32("hello")
	assert.Equal(t, CRC32("hello"), got, "must be deterministic")
	assert.NotEqual(t, CRC32("hello"), CRC32("world"), "distinct inputs differ")

	// The empty string hashes to 0.
	assert.Equal(t, "0", CRC32(""))
}

func TestQuoteGoflagsToken(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		want    string
		wantErr bool
	}{
		{name: "plain token unquoted", token: "-race", want: "-race"},
		{name: "token with space uses single quotes", token: "foo bar", want: "'foo bar'"},
		{name: "token with tab uses single quotes", token: "foo\tbar", want: "'foo\tbar'"},
		{name: "token with single quote uses double quotes", token: "it's", want: `"it's"`},
		{name: "token with double quote uses single quotes", token: `say "hi"`, want: `'say "hi"'`},
		{name: "token with both quotes errors", token: `a ' " b`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QuoteGoflagsToken(tt.token)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewFileScanner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lines.txt")
	require.NoError(t, os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	// Advance the offset first; NewFileScanner must seek back to the start.
	_, err = f.Seek(3, 0)
	require.NoError(t, err)

	scanner, err := NewFileScanner(f, 4096)
	require.NoError(t, err)

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	require.NoError(t, scanner.Err())
	assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
}

func TestContextLogger(t *testing.T) {
	t.Run("round-trips a stored logger", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		ctx := ContextWithLogger(context.Background(), logger)
		assert.Same(t, logger, LoggerFromContext(ctx))
	})

	t.Run("returns default logger when absent", func(t *testing.T) {
		assert.Same(t, slog.Default(), LoggerFromContext(context.Background()))
	})
}

func TestIsUnixAndIsWindows(t *testing.T) {
	// Exactly the current platform family must report true; the two are
	// mutually exclusive on every supported OS.
	assert.NotEqual(t, IsUnix(), IsWindows())
}

func TestNormalizePath(t *testing.T) {
	// NormalizePath cleans and converts separators to forward slashes.
	assert.Equal(t, "a/b/c", NormalizePath("a/b/./c"))
	assert.Equal(t, "a/c", NormalizePath("a/b/../c"))
	assert.Equal(t, ".", NormalizePath(""))
}

func TestWriteFile(t *testing.T) {
	t.Run("writes content", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "out.txt")
		require.NoError(t, WriteFile(path, "hello world"))
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "hello world", string(data))
	})

	t.Run("errors on unwritable path", func(t *testing.T) {
		// A path whose parent directory does not exist cannot be created.
		bad := filepath.Join(t.TempDir(), "no-such-dir", "out.txt")
		require.Error(t, WriteFile(bad, "x"))
	})
}

func TestAssertPasses(t *testing.T) {
	// A satisfied assertion must not exit the process.
	assert.NotPanics(t, func() {
		Assert(true, "should not fail")
	})
}

func TestAssertType(t *testing.T) {
	// A matching type assertion returns the typed value.
	var v any = "hello"
	assert.Equal(t, "hello", AssertType[string](v))
}
