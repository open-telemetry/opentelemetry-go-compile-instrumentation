//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecordUntilNoteHot(t *testing.T) {
	const miss = "./extra.go:11:6: cannot inline Note: function too complex: cost 106 exceeds budget 80"
	const hit = "./extra.go:11:6: can inline Note with cost 106"

	tests := []struct {
		name         string
		outputs      []string
		wantAttempts int
		wantOut      string
		wantLogs     int
	}{
		{"first recording hot", []string{hit}, 1, hit, 0},
		{"one miss then hot", []string{miss, hit}, 2, hit, 1},
		{"two misses then hot", []string{miss, miss, hit}, 3, hit, 2},
		{"all recordings miss", []string{miss, miss, miss, hit}, maxNoteRecordings, miss, maxNoteRecordings},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recordings, compiles int
			var logs []string
			attempts, out := recordUntilNoteHot(
				func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
				func() { recordings++ },
				func() string {
					out := tt.outputs[compiles]
					compiles++
					return out
				},
			)
			assert.Equal(t, tt.wantAttempts, attempts)
			assert.Equal(t, tt.wantOut, out)
			assert.Equal(t, tt.wantAttempts, recordings, "every attempt records a fresh profile")
			assert.Equal(t, tt.wantAttempts, compiles, "every recording is checked by one profiled compile")
			assert.Len(t, logs, tt.wantLogs)
			if tt.wantLogs > 0 {
				assert.Contains(t, logs[len(logs)-1], fmt.Sprintf("recording %d/%d did not make Note hot", tt.wantLogs, maxNoteRecordings))
			}
		})
	}
}
