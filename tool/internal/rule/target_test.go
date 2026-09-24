// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestTargetUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    rule.Target
		wantErr string
	}{
		{name: "single", content: "net/http", want: rule.NewTarget("net/http")},
		{name: "blank", content: "'  '", want: rule.Target{}},
		{name: "null", content: "~", want: rule.Target{}},
		{name: "list", content: "[$root, main]", want: rule.NewTarget(rule.TargetRoot, "main")},
		{
			name:    "list with not",
			content: "- example.com/**\n- not: example.com/mock\n- not: main\n",
			want:    rule.Target{Include: []string{"example.com/**"}, Exclude: []string{"example.com/mock", "main"}},
		},
		{name: "mapping", content: "not: example.com/pkg", wantErr: "must be a pattern or a list of patterns"},
		{name: "unknown key", content: "- skip: example.com/pkg", wantErr: "must be a pattern or {not: pattern}"},
		{name: "nested list", content: "- [example.com/pkg]", wantErr: "must be a pattern or {not: pattern}"},
		{name: "not with a list", content: "- not: [a, b]", wantErr: "must be a pattern or {not: pattern}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got rule.Target
			err := yaml.Unmarshal([]byte(tt.content), &got)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTargetRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		target   rule.Target
		wantJSON string
	}{
		{name: "single stays a string", target: rule.NewTarget("net/http"), wantJSON: `"net/http"`},
		{name: "zero", target: rule.Target{}, wantJSON: `""`},
		{
			name:     "list",
			target:   rule.Target{Include: []string{rule.TargetRoot, "main"}, Exclude: []string{"example.com/mock"}},
			wantJSON: `["$root","main",{"not":"example.com/mock"}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.target)
			require.NoError(t, err)
			assert.JSONEq(t, tt.wantJSON, string(data))

			var fromJSON rule.Target
			require.NoError(t, json.Unmarshal(data, &fromJSON))
			assert.Equal(t, tt.target, fromJSON)

			out, err := yaml.Marshal(tt.target)
			require.NoError(t, err)
			var fromYAML rule.Target
			require.NoError(t, yaml.Unmarshal(out, &fromYAML))
			assert.Equal(t, tt.target, fromYAML)
		})
	}
}

func TestTargetUnmarshalJSONRejectsInvalidEntries(t *testing.T) {
	for _, content := range []string{`{"not":"x"}`, `[{"skip":"x"}]`, `[{"not":""}]`, `[1]`} {
		var got rule.Target
		require.Error(t, json.Unmarshal([]byte(content), &got), content)
	}
}

func TestTargetValidate(t *testing.T) {
	tests := []struct {
		name    string
		target  rule.Target
		wantErr string
	}{
		{name: "single", target: rule.NewTarget("net/http")},
		{
			name:   "list",
			target: rule.Target{Include: []string{rule.TargetRoot}, Exclude: []string{"main", rule.TargetRoot}},
		},
		{name: "zero", target: rule.Target{}, wantErr: "target is required"},
		{name: "only not", target: rule.Target{Exclude: []string{"main"}}, wantErr: "selects no package"},
		{name: "empty pattern", target: rule.NewTarget("net/http", " "), wantErr: "empty pattern"},
		{
			name:    "invalid excluded glob",
			target:  rule.Target{Include: []string{"example.com/**"}, Exclude: []string{"example.com/[x"}},
			wantErr: "not a valid glob pattern",
		},
		{name: "root inside a glob", target: rule.NewTarget("$root/**"), wantErr: `must be exactly "$root"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.target.Validate()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestTargetMatches(t *testing.T) {
	roots := []string{"example.com/app"}
	tests := []struct {
		name       string
		target     rule.Target
		importPath string
		roots      []string
		want       bool
	}{
		{name: "exact", target: rule.NewTarget("net/http"), importPath: "net/http", want: true},
		{name: "exact miss", target: rule.NewTarget("net/http"), importPath: "net/url"},
		{name: "glob", target: rule.NewTarget("example.com/svc/*"), importPath: "example.com/svc/users", want: true},
		{
			name:       "any entry of a list",
			target:     rule.NewTarget("net/url", "net/http"),
			importPath: "net/http",
			want:       true,
		},
		{
			name:       "root",
			target:     rule.NewTarget(rule.TargetRoot),
			importPath: "example.com/app/lib",
			roots:      roots,
			want:       true,
		},
		{name: "root without roots", target: rule.NewTarget(rule.TargetRoot), importPath: "example.com/app/lib"},
		{name: "root does not reach main", target: rule.NewTarget(rule.TargetRoot), importPath: "main", roots: roots},
		{
			name:       "root and main",
			target:     rule.NewTarget(rule.TargetRoot, "main"),
			importPath: "main",
			roots:      roots,
			want:       true,
		},
		{
			name:       "excluded",
			target:     rule.Target{Include: []string{"example.com/app/**"}, Exclude: []string{"example.com/app/mock"}},
			importPath: "example.com/app/mock",
		},
		{
			name:       "not excluded",
			target:     rule.Target{Include: []string{"example.com/app/**"}, Exclude: []string{"example.com/app/mock"}},
			importPath: "example.com/app/lib",
			want:       true,
		},
		{
			name:       "excluded main",
			target:     rule.Target{Include: []string{"**"}, Exclude: []string{"main"}},
			importPath: "main",
		},
		{
			name:       "excluded root",
			target:     rule.Target{Include: []string{"example.com/**"}, Exclude: []string{rule.TargetRoot}},
			importPath: "example.com/app/lib",
			roots:      roots,
		},
		{
			name:       "dependency outside an excluded root",
			target:     rule.Target{Include: []string{"example.com/**"}, Exclude: []string{rule.TargetRoot}},
			importPath: "example.com/lib",
			roots:      roots,
			want:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.target.Matches(tt.importPath, tt.roots))
		})
	}
}

func TestTargetExact(t *testing.T) {
	exact := rule.NewTarget("net/http")
	path, ok := exact.Exact()
	assert.True(t, ok)
	assert.Equal(t, "net/http", path)

	for _, target := range []rule.Target{
		rule.NewTarget("example.com/*"),
		rule.NewTarget(rule.TargetRoot),
		rule.NewTarget("net/http", "net/url"),
		{Include: []string{"net/http"}, Exclude: []string{"net/url"}},
	} {
		_, ok = target.Exact()
		assert.False(t, ok, target.String())
	}
}

func TestTargetString(t *testing.T) {
	single := rule.NewTarget("net/http")
	assert.Equal(t, "net/http", single.String())
	list := rule.Target{Include: []string{rule.TargetRoot, "main"}, Exclude: []string{"example.com/mock"}}
	assert.Equal(t, "[$root, main, not example.com/mock]", list.String())
}
