// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type matchRow struct {
	Package string  `json:"package"`
	Rule    string  `json:"rule"`
	Phase   string  `json:"phase"`
	Source  string  `json:"source"`
	Matched bool    `json:"matched"`
	Reason  *string `json:"reason"`
}

func readMatchRows(t *testing.T, file string) []matchRow {
	t.Helper()
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	var rows []matchRow
	for line := range strings.Lines(string(data)) {
		var row matchRow
		require.NoError(t, json.Unmarshal([]byte(line), &row))
		rows = append(rows, row)
	}
	return rows
}

func TestWriteMatchDecision(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "debug", "example.com_p")
	r := &InstFuncRule{InstBaseRule: InstBaseRule{Name: "candidate"}}

	// Setup decisions append to one stream per package.
	require.NoError(t, WriteMatchDecision(dir, MatchDecision{
		Phase: "setup", Package: "example.com/p", Source: "/src/a.go", Rule: r,
	}))
	require.NoError(t, WriteMatchDecision(dir, MatchDecision{
		Phase: "setup", Package: "example.com/p", Rule: r, Reason: "function_not_found",
	}))
	rows := readMatchRows(t, filepath.Join(dir, "setup.matches.jsonl"))
	require.Len(t, rows, 2)
	require.Equal(t, matchRow{
		Package: "example.com/p", Rule: "candidate", Phase: "setup", Source: "/src/a.go", Matched: true,
	}, rows[0])
	require.False(t, rows[1].Matched)
	require.NotNil(t, rows[1].Reason)
	require.Equal(t, "function_not_found", *rows[1].Reason)
	require.Empty(t, rows[1].Source)

	// Instrument decisions go to a stream named after the source file.
	require.NoError(t, WriteMatchDecision(dir, MatchDecision{
		Phase: "instrument", Package: "example.com/p", Source: filepath.Join("work", "b.go"),
		Rule: r, Reason: "ignored",
	}))
	rows = readMatchRows(t, filepath.Join(dir, "b.go.matches.jsonl"))
	require.Len(t, rows, 1)
	require.Equal(t, "instrument", rows[0].Phase)
	require.Equal(t, filepath.Join("work", "b.go"), rows[0].Source)
	require.Equal(t, "ignored", *rows[0].Reason)
	require.Len(t, readMatchRows(t, filepath.Join(dir, "setup.matches.jsonl")), 2)
}

func TestWriteMatchDecisionErrors(t *testing.T) {
	r := &InstFuncRule{InstBaseRule: InstBaseRule{Name: "candidate"}}
	d := MatchDecision{Phase: "setup", Package: "example.com/p", Rule: r}

	// The debug directory cannot be created below a regular file.
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	require.Error(t, WriteMatchDecision(filepath.Join(blocker, "debug"), d))

	// The stream cannot be opened when a directory has its name.
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "setup.matches.jsonl"), 0o755))
	require.Error(t, WriteMatchDecision(dir, d))
}
