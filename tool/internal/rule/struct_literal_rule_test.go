// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewInstStructLiteralRule(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		expectError bool
		validate    func(*testing.T, *InstStructLiteralRule)
	}{
		{
			name: "valid rule",
			yaml: `
target: "*"
struct_literal: "net/http.Server"
match: pointer-only
template: |
  func(s *http.Server) *http.Server {
      return s
  }({{ . }})
`,
			expectError: false,
			validate: func(t *testing.T, r *InstStructLiteralRule) {
				assert.Equal(t, "net/http.Server", r.StructLiteral)
				assert.Equal(t, "pointer-only", r.Match)
				assert.Contains(t, r.Template, "func(s *http.Server)")
			},
		},
		{
			name: "default match",
			yaml: `
target: "*"
struct_literal: "net/http.Server"
template: "wrapped({{ . }})"
`,
			expectError: false,
			validate: func(t *testing.T, r *InstStructLiteralRule) {
				assert.Equal(t, "any", r.Match)
			},
		},
		{
			name: "invalid match",
			yaml: `
target: "*"
struct_literal: "net/http.Server"
match: "something-else"
template: "wrapped({{ . }})"
`,
			expectError: true,
		},
		{
			name: "missing struct_literal",
			yaml: `
target: "*"
template: "wrapped({{ . }})"
`,
			expectError: true,
		},
		{
			name: "missing template",
			yaml: `
target: "*"
struct_literal: "net/http.Server"
`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewInstStructLiteralRule([]byte(tt.yaml), "test-rule")
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, r)
				}
			}
		})
	}
}
