// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
