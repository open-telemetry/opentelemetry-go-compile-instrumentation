// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantVersion string
		wantLegacy  bool
		wantRules   []string
		wantErr     string
	}{
		{
			name: "versioned",
			content: `version: "v1.1.0"
rule:
  target: example.com/pkg
  version: v2.0.0,v3.0.0
`,
			wantVersion: "v1.1.0",
			wantRules:   []string{"rule"},
		},
		{
			name: "legacy",
			content: `rule:
  target: example.com/pkg
`,
			wantVersion: LegacyVersion,
			wantLegacy:  true,
			wantRules:   []string{"rule"},
		},
		{
			name: "sorted entries",
			content: `zebra:
  target: example.com/zebra
alpha:
  target: example.com/alpha
`,
			wantVersion: LegacyVersion,
			wantLegacy:  true,
			wantRules:   []string{"alpha", "zebra"},
		},
		{name: "empty", wantVersion: LegacyVersion, wantLegacy: true},
		{name: "invalid yaml", content: "rule: {", wantErr: "did not find expected node content"},
		{name: "non mapping root", content: "- rule", wantErr: "root must be a mapping"},
		{name: "numeric key", content: "1: {}", wantErr: "mapping keys must be strings"},
		{name: "complex key", content: "? [rule]\n: {}", wantErr: "mapping keys must be strings"},
		{name: "missing v prefix", content: `version: "1.0.0"`, wantErr: "not a valid release version"},
		{name: "empty version", content: `version: ""`, wantErr: "not a valid release version"},
		{name: "numeric version", content: `version: 1`, wantErr: "must be a string"},
		{name: "mapping version", content: "version:\n  major: 1", wantErr: "must be a string"},
		{
			name: "duplicate rule",
			content: `rule:
  target: example.com/first
rule:
  target: example.com/second
`,
			wantErr: `mapping key "rule" already defined`,
		},
		{
			name: "duplicate version",
			content: `version: "v1.0.0"
version: "v1.1.0"
`,
			wantErr: `mapping key "version" already defined`,
		},
		{
			name:    "pseudo version",
			content: `version: "v1.1.1-0.20260827000000-abcdefabcdef"`,
			wantErr: "not a valid release version",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := ParseFile([]byte(test.content))
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantVersion, doc.MinimumVersion)
			assert.Equal(t, test.wantLegacy, doc.Legacy)
			assert.Equal(t, test.wantRules, entryNames(doc.Entries))
		})
	}
}

func TestFileRules(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantType any
		wantErr  string
	}{
		{
			name:     "struct",
			content:  "rule:\n  target: example.com/pkg\n  struct: Server\n",
			wantType: &InstStructRule{},
		},
		{
			name:     "file",
			content:  "rule:\n  target: example.com/pkg\n  file: hook.go\n  path: example.com/hooks\n",
			wantType: &InstFileRule{},
		},
		{
			name:     "directive",
			content:  "rule:\n  target: example.com/pkg\n  directive: otelc:span\n  template: _ = 0\n",
			wantType: &InstDirectiveRule{},
		},
		{
			name:     "raw",
			content:  "rule:\n  target: example.com/pkg\n  func: Run\n  raw: _ = 0\n",
			wantType: &InstRawRule{},
		},
		{
			name:     "function",
			content:  "rule:\n  target: example.com/pkg\n  func: Run\n  before: BeforeRun\n  path: example.com/hooks\n",
			wantType: &InstFuncRule{},
		},
		{
			name:     "function call",
			content:  "rule:\n  target: example.com/pkg\n  function_call: net/http.Get\n  replace: tracedGet({{ . }})\n",
			wantType: &InstCallRule{},
		},
		{
			name:     "struct literal",
			content:  "rule:\n  target: example.com/pkg\n  struct_literal: example.com/pkg.Client\n  field:\n    - name: Transport\n      value: tracedTransport\n",
			wantType: &InstLitRule{},
		},
		{
			name:     "identifier",
			content:  "rule:\n  target: example.com/pkg\n  identifier: DefaultClient\n  replace: tracedClient\n",
			wantType: &InstDeclRule{},
		},
		{
			name:    "normalize error",
			content: "rule:\n  target: example.com/pkg\n  where:\n    target: other.example/pkg\n  do:\n    inject_code:\n      raw: _ = 0\n",
			wantErr: "target must be top-level",
		},
		{
			name:    "constructor error",
			content: "rule:\n  target: example.com/pkg\n  directive: ''\n",
			wantErr: "directive",
		},
		{
			name:    "unrecognized selector",
			content: "rule:\n  target: example.com/pkg\n",
			wantErr: "no recognised selector",
		},
		{
			name:    "empty target",
			content: "rule:\n  target: '  '\n  func: Run\n  raw: _ = 0\n",
			wantErr: "empty target",
		},
		{
			name:    "invalid glob",
			content: "rule:\n  target: example.com/[pkg\n  func: Run\n  raw: _ = 0\n",
			wantErr: "not a valid glob pattern",
		},
		{
			name:    "invalid version range",
			content: "rule:\n  target: example.com/pkg\n  version: v1.0.0,\n  func: Run\n  raw: _ = 0\n",
			wantErr: "non-empty start and end bounds",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file, err := ParseFile([]byte(test.content))
			require.NoError(t, err)

			rules, err := file.Rules()
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, rules, 1)
			assert.IsType(t, test.wantType, rules[0])
		})
	}
}

func TestFileRulesDecodeError(t *testing.T) {
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("- value"), &node))
	file := File{Entries: []Entry{{Name: "rule", Node: *node.Content[0]}}}

	_, err := file.Rules()
	require.ErrorContains(t, err, `parsing rule "rule"`)
}

func TestCheckVersion(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		required string
		wantErr  bool
	}{
		{name: "equal", current: "v1.1.0", required: "v1.1.0"},
		{name: "newer", current: "v1.2.0", required: "v1.1.0"},
		{name: "older", current: "v1.0.0", required: "v1.1.0", wantErr: true},
		{name: "prerelease older", current: "v1.1.0-rc.1", required: "v1.1.0", wantErr: true},
		{name: "development default", current: "v0.0.0", required: "v9.0.0"},
		{name: "devel", current: "(devel)", required: "v9.0.0"},
		{name: "empty", current: "", required: "v9.0.0"},
		{name: "custom development build", current: "custom", required: "v9.0.0"},
		{
			name:     "pseudo version",
			current:  "v1.1.1-0.20260827000000-abcdefabcdef",
			required: "v9.0.0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := CheckVersion(test.current, test.required)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func entryNames(entries []Entry) []string {
	if entries == nil {
		return nil
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name
	}
	return names
}
