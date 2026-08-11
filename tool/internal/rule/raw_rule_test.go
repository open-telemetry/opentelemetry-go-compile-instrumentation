// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstRawRule(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		ruleID  string
		wantErr bool
		check   func(*testing.T, *InstRawRule)
	}{
		{
			name:   "minimal valid rule",
			ruleID: "raw1",
			yaml: `
target: main
func: Bar
raw: println("hi")
`,
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "raw1", r.Name)
				assert.Equal(t, "main", r.Target)
				assert.Equal(t, "Bar", r.Func)
				assert.Equal(t, `println("hi")`, r.Raw)
			},
		},
		{
			name:   "explicit name is preserved",
			ruleID: "fallback",
			yaml: `
name: explicit
target: main
func: Bar
raw: println()
`,
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "explicit", r.Name)
			},
		},
		{
			name:   "valid placement before with pattern",
			ruleID: "raw2",
			yaml: `
target: main
func: Bar
recv: "*Recv"
raw: println()
pattern: "^name := getName\\(\\)$"
placement: before
`,
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "*Recv", r.Recv)
				assert.Equal(t, "before", r.Placement)
				assert.Equal(t, `^name := getName\(\)$`, r.Pattern)
			},
		},
		{
			name:   "valid placement after",
			ruleID: "raw3",
			yaml: `
target: main
func: Bar
raw: println()
placement: after
`,
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "after", r.Placement)
			},
		},
		{
			name:    "empty raw is rejected",
			ruleID:  "raw4",
			yaml:    "target: main\nfunc: Bar\nraw: \"   \"",
			wantErr: true,
		},
		{
			name:    "missing raw is rejected",
			ruleID:  "raw5",
			yaml:    "target: main\nfunc: Bar",
			wantErr: true,
		},
		{
			name:    "invalid regex pattern is rejected",
			ruleID:  "raw6",
			yaml:    "target: main\nfunc: Bar\nraw: println()\npattern: \"(unclosed\"",
			wantErr: true,
		},
		{
			name:    "invalid placement value is rejected",
			ruleID:  "raw7",
			yaml:    "target: main\nfunc: Bar\nraw: println()\nplacement: sideways",
			wantErr: true,
		},
		{
			name:    "malformed yaml is rejected",
			ruleID:  "raw8",
			yaml:    "target: main\n  bad: [unterminated",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewInstRawRule([]byte(tt.yaml), tt.ruleID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, r)
			if tt.check != nil {
				tt.check(t, r)
			}
		})
	}
}
