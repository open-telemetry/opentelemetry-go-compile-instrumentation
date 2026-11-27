// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package compileindex

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
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

func TestStoreFromArtifactsMatchesStore(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	sets := []*rule.InstRuleSet{
		rule.NewInstRuleSet("net/http"),
		rule.NewInstRuleSet("example.org/a<b>"),
		rule.NewInstRuleSet("net/http"),
	}
	matched, err := json.Marshal(sets)
	require.NoError(t, err)
	names, err := json.Marshal(map[string]string{"net/http": "http"})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), matched, 0o644))
	require.NoError(t, os.WriteFile(util.GetImportNamesFile(), names, 0o644))

	require.NoError(t, Store())
	want, err := os.ReadFile(filename())
	require.NoError(t, err)

	require.NoError(t, os.Remove(filename()))
	require.NoError(t, StoreFromArtifacts(matched, names, sets))
	got, err := os.ReadFile(filename())
	require.NoError(t, err)

	assert.Equal(t, want, got)
	assert.False(t, Excludes("net/http"))
	assert.False(t, Excludes("example.org/a<b>"))
	assert.True(t, Excludes("unmatched"))

	require.Error(t, StoreFromArtifacts(matched, names, []*rule.InstRuleSet{nil}))
}

func TestLookupAndBuildUsesIgnoreDirective(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	normal := rule.NewInstRuleSet("example.com/lib")
	testSet := rule.NewInstRuleSet("example.com/lib")
	testSet.IsTest = true
	other := rule.NewInstRuleSet("example.com/other")
	other.AddIgnoredCallFile(filepath.Join(t.TempDir(), "other.go"))

	sets := []*rule.InstRuleSet{normal, testSet, other}
	matched, err := json.Marshal(sets)
	require.NoError(t, err)
	names := []byte(`{}`)
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), matched, 0o644))
	require.NoError(t, os.WriteFile(util.GetImportNamesFile(), names, 0o644))
	require.NoError(t, StoreFromArtifacts(matched, names, sets))

	// Candidate package without IgnoredCallFiles still sees buildUsesIgnore=true,
	// and its shard contains both normal and test variants.
	got, ok := Lookup("example.com/lib")
	require.True(t, ok)
	assert.False(t, got.Excluded)
	assert.True(t, got.BuildUsesIgnoreDirective)
	require.Len(t, got.Sets, 2)
	assert.False(t, got.Sets[0].IsTest)
	assert.True(t, got.Sets[1].IsTest)

	got, ok = Lookup("example.com/unmatched")
	require.True(t, ok)
	assert.True(t, got.Excluded)
}

func TestLookupFallsBackOnCorruptOrStaleArtifacts(t *testing.T) {
	setup := func(t *testing.T) {
		t.Helper()
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
		sets := []*rule.InstRuleSet{rule.NewInstRuleSet("example.com/lib")}
		matched, err := json.Marshal(sets)
		require.NoError(t, err)
		names := []byte(`{}`)
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), matched, 0o644))
		require.NoError(t, os.WriteFile(util.GetImportNamesFile(), names, 0o644))
		require.NoError(t, StoreFromArtifacts(matched, names, sets))
	}

	mutateShard := func(t *testing.T, fn func(*shardDocument)) {
		t.Helper()
		shardPath := shardFilename("example.com/lib")
		raw, err := os.ReadFile(shardPath)
		require.NoError(t, err)
		var shard shardDocument
		require.NoError(t, json.Unmarshal(raw, &shard))
		fn(&shard)
		updated, err := json.Marshal(shard)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(shardPath, updated, 0o644))
	}

	t.Run("missing shard", func(t *testing.T) {
		setup(t)
		require.NoError(t, os.Remove(shardFilename("example.com/lib")))
		_, ok := Lookup("example.com/lib")
		assert.False(t, ok)
	})

	t.Run("corrupt shard", func(t *testing.T) {
		setup(t)
		require.NoError(t, os.WriteFile(shardFilename("example.com/lib"), []byte("corrupt"), 0o644))
		_, ok := Lookup("example.com/lib")
		assert.False(t, ok)
	})

	t.Run("tampered sets with stale digest", func(t *testing.T) {
		setup(t)
		mutateShard(t, func(shard *shardDocument) {
			shard.Sets = []byte("[]")
		})
		_, ok := Lookup("example.com/lib")
		assert.False(t, ok)
	})

	t.Run("mismatched matched_sha256 with valid digest", func(t *testing.T) {
		setup(t)
		mutateShard(t, func(shard *shardDocument) {
			shard.MatchedHash[0] ^= 0xff
			shard.Digest = sha256.Sum256(shard.Sets)
		})
		_, ok := Lookup("example.com/lib")
		assert.False(t, ok)
	})

	t.Run("wrong module_path in shard sets", func(t *testing.T) {
		setup(t)
		wrongSets, err := json.Marshal([]*rule.InstRuleSet{rule.NewInstRuleSet("example.com/other")})
		require.NoError(t, err)
		mutateShard(t, func(shard *shardDocument) {
			shard.Sets = wrongSets
			shard.Digest = sha256.Sum256(shard.Sets)
		})
		_, ok := Lookup("example.com/lib")
		assert.False(t, ok)
	})

	t.Run("modified matched.json", func(t *testing.T) {
		setup(t)
		updated, err := json.Marshal([]*rule.InstRuleSet{rule.NewInstRuleSet("example.com/new")})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), updated, 0o644))
		_, ok := Lookup("example.com/lib")
		assert.False(t, ok)
	})

	t.Run("re-store cleans stale shards", func(t *testing.T) {
		setup(t)
		stalePath := shardFilename("example.com/lib")
		assert.FileExists(t, stalePath)

		sets := []*rule.InstRuleSet{rule.NewInstRuleSet("example.com/other")}
		matched, err := json.Marshal(sets)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), matched, 0o644))
		require.NoError(t, StoreFromArtifacts(matched, []byte(`{}`), sets))

		assert.NoFileExists(t, stalePath)
		assert.FileExists(t, shardFilename("example.com/other"))
	})

	t.Run("non-array shard sets with valid digest", func(t *testing.T) {
		setup(t)
		mutateShard(t, func(shard *shardDocument) {
			shard.Sets = []byte("42")
			shard.Digest = sha256.Sum256(shard.Sets)
		})
		_, ok := Lookup("example.com/lib")
		assert.False(t, ok)
	})

	t.Run("StoreFromArtifacts rejects nil set and unwritable shard dir", func(t *testing.T) {
		setup(t)
		require.Error(t, StoreFromArtifacts([]byte("[]"), []byte(`{}`), []*rule.InstRuleSet{nil}))

		badWorkDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, badWorkDir)
		require.NoError(t, os.WriteFile(util.GetBuildTempDir(), []byte("file"), 0o644))
		require.Error(t, StoreFromArtifacts([]byte("[]"), []byte(`{}`), nil))

		if runtime.GOOS != "windows" && os.Geteuid() != 0 {
			roWorkDir := t.TempDir()
			t.Setenv(util.EnvOtelcWorkDir, roWorkDir)
			buildDir := util.GetBuildTempDir()
			require.NoError(t, os.MkdirAll(buildDir, 0o755))
			require.NoError(t, os.Chmod(buildDir, 0o555))
			t.Cleanup(func() { _ = os.Chmod(buildDir, 0o755) })
			require.Error(t, StoreFromArtifacts([]byte("[]"), []byte(`{}`), nil))
		}
	})
}

func BenchmarkCompileIndexExcludes(b *testing.B) {
	b.Setenv(util.EnvOtelcWorkDir, b.TempDir())
	if err := os.MkdirAll(util.GetBuildTempDir(), 0o755); err != nil {
		b.Fatal(err)
	}
	sets := make([]*rule.InstRuleSet, 0, 50)
	for i := range 50 {
		sets = append(sets, rule.NewInstRuleSet("example.com/pkg/"+string(rune('a'+i/26))+string(rune('a'+i%26))))
	}
	matched, err := json.Marshal(sets)
	if err != nil {
		b.Fatal(err)
	}
	names := []byte(`{}`)
	if err = os.WriteFile(util.GetMatchedRuleFile(), matched, 0o644); err != nil {
		b.Fatal(err)
	}
	if err = os.WriteFile(util.GetImportNamesFile(), names, 0o644); err != nil {
		b.Fatal(err)
	}
	if err = StoreFromArtifacts(matched, names, sets); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if !Excludes("example.com/unmatched/pkg") {
			b.Fatal("expected package to be excluded")
		}
	}
}

func BenchmarkLoadPackageRules(b *testing.B) {
	b.Setenv(util.EnvOtelcWorkDir, b.TempDir())
	if err := os.MkdirAll(util.GetBuildTempDir(), 0o755); err != nil {
		b.Fatal(err)
	}
	srcRoot := b.TempDir()
	sets := make([]*rule.InstRuleSet, 0, 100)
	for i := range 100 {
		pkg := "example.com/pkg/" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		rs := rule.NewInstRuleSet(pkg)
		rs.AddFuncRule(filepath.Join(srcRoot, pkg, "file.go"), &rule.InstFuncRule{
			InstBaseRule: rule.InstBaseRule{Name: "rule-" + pkg},
			Func:         "Handle",
		})
		sets = append(sets, rs)
	}
	matched, err := json.Marshal(sets)
	if err != nil {
		b.Fatal(err)
	}
	names := []byte(`{}`)
	if err = os.WriteFile(util.GetMatchedRuleFile(), matched, 0o644); err != nil {
		b.Fatal(err)
	}
	if err = os.WriteFile(util.GetImportNamesFile(), names, 0o644); err != nil {
		b.Fatal(err)
	}
	if err = StoreFromArtifacts(matched, names, sets); err != nil {
		b.Fatal(err)
	}

	target := sets[50].ModulePath

	b.Run("shard", func(b *testing.B) {
		for b.Loop() {
			got, ok := Lookup(target)
			if !ok || got.Excluded || len(got.Sets) != 1 {
				b.Fatalf("unexpected shard load result: ok=%v excluded=%v len=%d", ok, got.Excluded, len(got.Sets))
			}
		}
	})

	b.Run("full_matched_json", func(b *testing.B) {
		matchedPath := util.GetMatchedRuleFile()
		for b.Loop() {
			data, readErr := os.ReadFile(matchedPath)
			if readErr != nil {
				b.Fatal(readErr)
			}
			var all []*rule.InstRuleSet
			if jsonErr := json.Unmarshal(data, &all); jsonErr != nil {
				b.Fatal(jsonErr)
			}
		}
	})
}
