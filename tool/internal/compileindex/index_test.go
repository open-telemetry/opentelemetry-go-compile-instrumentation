// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package compileindex

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func storeFixture(t *testing.T) {
	t.Helper()
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(),
		[]byte(`[{"module_path":"net\u002fhttp"},{"MODULE_PATH":"main"},
		{"module\u005fpath":"example.org/a\u003cb\u003e"},{"module_path":"net/http"}]`), 0o644))
	require.NoError(t, os.WriteFile(util.GetImportNamesFile(), []byte(`{"net/http":"http"}`), 0o644))
	require.NoError(t, Store())
}

func TestExcludes(t *testing.T) {
	storeFixture(t)
	for _, candidate := range []string{"net/http", "main", "example.org/a<b>", ""} {
		assert.False(t, Excludes(candidate), candidate)
	}
	assert.True(t, Excludes("net"))
	assert.True(t, Excludes("net/http/pprof"))

	data, err := os.ReadFile(filename())
	require.NoError(t, err)
	var doc document
	require.NoError(t, json.Unmarshal(data, &doc))
	var idx index
	require.NoError(t, json.Unmarshal(doc.Index, &idx))
	assert.Equal(t, []string{"example.org/a<b>", "main", "net/http"}, idx.Packages)
}

func TestExcludesFallsBackForChangedArtifacts(t *testing.T) {
	for _, artifact := range []string{"matched", "names"} {
		for _, state := range []string{"missing", "malformed", "changed-valid", "directory"} {
			t.Run(artifact+"/"+state, func(t *testing.T) {
				storeFixture(t)
				path := util.GetMatchedRuleFile()
				if artifact == "names" {
					path = util.GetImportNamesFile()
				}
				require.NoError(t, os.Remove(path))
				switch state {
				case "malformed":
					require.NoError(t, os.WriteFile(path, []byte("not JSON"), 0o644))
				case "changed-valid":
					require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o644))
				case "directory":
					require.NoError(t, os.Mkdir(path, 0o755))
				}
				assert.False(t, Excludes("unmatched"))
			})
		}
	}
}

func TestExcludesFallsBackForIndexProblems(t *testing.T) {
	for _, state := range []string{"missing", "truncated", "wrong-shape", "edited-packages", "unknown-version"} {
		t.Run(state, func(t *testing.T) {
			storeFixture(t)
			data, err := os.ReadFile(filename())
			require.NoError(t, err)
			switch state {
			case "missing":
				require.NoError(t, os.Remove(filename()))
			case "truncated":
				require.NoError(t, os.WriteFile(filename(), data[:len(data)/2], 0o644))
			case "wrong-shape":
				require.NoError(t, os.WriteFile(filename(), []byte(`[]`), 0o644))
			default:
				var doc document
				require.NoError(t, json.Unmarshal(data, &doc))
				var idx index
				require.NoError(t, json.Unmarshal(doc.Index, &idx))
				idx.Packages = nil
				if state == "unknown-version" {
					idx.Version++
				}
				doc.Index, err = json.Marshal(idx)
				require.NoError(t, err)
				if state == "unknown-version" {
					doc.Digest = sha256.Sum256(doc.Index)
				}
				data, err = json.Marshal(doc)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filename(), data, 0o644))
			}
			assert.False(t, Excludes("unmatched"))
			assert.False(t, Excludes("net/http"))
		})
	}
}

func TestStoreErrors(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.Error(t, Store())
	storeFixture(t)
	require.NoError(t, os.Remove(util.GetImportNamesFile()))
	require.Error(t, Store())
	require.NoError(t, os.WriteFile(util.GetImportNamesFile(), []byte(`{}`), 0o644))
	require.NoError(t, os.Remove(filename()))
	require.NoError(t, os.Mkdir(filename(), 0o755))
	require.Error(t, Store())
}

func TestStoreRejectsInvalidArtifacts(t *testing.T) {
	for _, artifact := range []string{"matched", "names"} {
		for _, content := range []string{"not JSON", "42", "[null]"} {
			t.Run(artifact+"/"+content, func(t *testing.T) {
				storeFixture(t)
				path := util.GetMatchedRuleFile()
				if artifact == "names" {
					path = util.GetImportNamesFile()
				}
				require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
				require.Error(t, Store())
				assert.False(t, Excludes("unmatched"))
			})
		}
	}
}
