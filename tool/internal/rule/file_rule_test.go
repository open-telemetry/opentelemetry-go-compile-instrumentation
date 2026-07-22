// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstFileRule(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		check   func(*testing.T, *InstFileRule)
	}{
		{
			name: "valid rule",
			yaml: `
file: my_file.go
target: example.com/pkg
path: github.com/example/pkg
`,
			check: func(t *testing.T, r *InstFileRule) {
				assert.Equal(t, "my_file.go", r.File)
				assert.Equal(t, "example.com/pkg", r.Target)
				assert.Equal(t, "github.com/example/pkg", r.Path)
			},
		},
		{
			name:    "missing file field",
			yaml:    `target: example.com/pkg\npath: github.com/example/pkg`,
			wantErr: true,
		},
		{
			name:    "missing path field",
			yaml:    `target: example.com/pkg\nfile: my_file.go`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := newFileRuleFromFlat(t, tt.name, tt.yaml)
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

// newFileRuleFromFlat builds an InstFileRule from a flat fixture. File rules
// take no where selectors; file/path are the add_file payload.
func newFileRuleFromFlat(t *testing.T, name, yamlStr string) (*InstFileRule, error) {
	t.Helper()
	flat, err := flatMap(yamlStr)
	if err != nil {
		return nil, err
	}
	base, rest := splitBase(flat, name)
	act := &FileAction{}
	if v, ok := rest["file"].(string); ok {
		act.File = v
		delete(rest, "file")
	}
	if v, ok := rest["path"].(string); ok {
		act.Path = v
		delete(rest, "path")
	}
	return NewInstFileRule(base, mapNode(t, rest), act)
}
