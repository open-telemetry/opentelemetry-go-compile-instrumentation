// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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
		{
			name: "module defaults to path",
			yaml: `
file: my_file.go
target: example.com/pkg
path: github.com/example/instrumentation/net/http/client
`,
			check: func(t *testing.T, r *InstFileRule) {
				assert.Equal(t,
					"github.com/example/instrumentation/net/http/client",
					r.Path,
				)
				assert.Equal(t, r.Path, r.ModulePath)
			},
		},
		{
			name: "import path not part of module path",
			yaml: `
file: my_file.go
target: example.com/pkg
path: github.com/example/instrumentation/net/http/client
module: github.com/example/pkg
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fields map[string]any
			_ = yaml.Unmarshal([]byte(tt.yaml), &fields)
			data, _ := yaml.Marshal(fields)

			r, err := NewInstFileRule(data, tt.name)
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
