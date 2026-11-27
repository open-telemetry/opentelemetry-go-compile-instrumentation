// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package compileindex stores an optional setup-produced routing index and
// per-package rule shards. Index and shard entries are trusted only while
// the setup artifacts still match their recorded digests.
package compileindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const version = 2

// index is derived data, not a replacement for the matched rule sets. In
// particular, candidate compiles still use the existing file/test/cgo matcher.
type index struct {
	Version                  int      `json:"version"`
	Packages                 []string `json:"packages"`
	MatchedHash              [32]byte `json:"matched_sha256"`
	NamesHash                [32]byte `json:"names_sha256"`
	BuildUsesIgnoreDirective bool     `json:"build_uses_ignore_directive,omitzero"`
}

// shardDocument wraps a single package's sharded rule sets with its own
// integrity digest and the matched.json digest it was derived from.
type shardDocument struct {
	MatchedHash [32]byte        `json:"matched_sha256"`
	Sets        json.RawMessage `json:"sets"`
	Digest      [32]byte        `json:"sha256"`
}

// document detects corruption of the routing metadata itself, including a
// valid-JSON edit that removes a package but leaves artifact hashes unchanged.
type document struct {
	Index  json.RawMessage `json:"index"`
	Digest [32]byte        `json:"sha256"`
}

func filename() string { return util.GetBuildTemp("compile_index.json") }

func packagesDir() string { return util.GetInvocationTemp("packages") }

func shardFilename(importPath string) string {
	sum := sha256.Sum256([]byte(importPath))
	return filepath.Join(packagesDir(), hex.EncodeToString(sum[:])+".json")
}

// Store is a test and recovery helper that publishes the index from the
// on-disk matched rules and import names artifacts. Production setup uses
// StoreFromArtifacts.
func Store() error {
	matched, err := os.ReadFile(util.GetMatchedRuleFile())
	if err != nil {
		return ex.Wrapf(err, "reading matched rules for compile index")
	}
	names, err := os.ReadFile(util.GetImportNamesFile())
	if err != nil {
		return ex.Wrapf(err, "reading import names for compile index")
	}
	sets, err := setsFromArtifacts(matched, names)
	if err != nil {
		return err
	}
	return writeIndexAndShards(matched, names, sets)
}

// StoreFromArtifacts publishes the index from already-marshaled setup
// artifacts and rule sets, avoiding a redundant read and JSON decode.
func StoreFromArtifacts(matched, names []byte, sets []*rule.InstRuleSet) error {
	return writeIndexAndShards(matched, names, sets)
}

func writeIndexAndShards(matched, names []byte, sets []*rule.InstRuleSet) error {
	packages, err := packagesFromSets(sets)
	if err != nil {
		return err
	}
	matchedHash := sha256.Sum256(matched)
	buildUsesIgnore, err := writeShards(sets, matchedHash)
	if err != nil {
		return err
	}
	data, err := json.Marshal(index{
		Version:                  version,
		Packages:                 packages,
		MatchedHash:              matchedHash,
		NamesHash:                sha256.Sum256(names),
		BuildUsesIgnoreDirective: buildUsesIgnore,
	})
	if err != nil {
		return ex.Wrapf(err, "encoding compile index")
	}
	data, err = json.Marshal(document{Index: data, Digest: sha256.Sum256(data)})
	if err != nil {
		return ex.Wrapf(err, "encoding compile index document")
	}
	if err = util.WriteFileAtomic(filename(), data); err != nil {
		return ex.Wrapf(err, "writing compile index")
	}
	return nil
}

func writeShards(sets []*rule.InstRuleSet, matchedHash [32]byte) (bool, error) {
	dir := packagesDir()
	if err := os.RemoveAll(dir); err != nil {
		return false, ex.Wrapf(err, "cleaning compile index shard directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, ex.Wrapf(err, "creating compile index shard directory")
	}

	grouped := make(map[string][]*rule.InstRuleSet)
	buildUsesIgnore := false
	for _, set := range sets {
		grouped[set.ModulePath] = append(grouped[set.ModulePath], set)
		if len(set.IgnoredCallFiles) > 0 {
			buildUsesIgnore = true
		}
	}

	for modPath, pkgSets := range grouped {
		rawSets, err := json.Marshal(pkgSets)
		if err != nil {
			return false, ex.Wrapf(err, "encoding compile index shard for %s", modPath)
		}
		docBytes, err := json.Marshal(shardDocument{
			MatchedHash: matchedHash,
			Sets:        rawSets,
			Digest:      sha256.Sum256(rawSets),
		})
		if err != nil {
			return false, ex.Wrapf(err, "encoding compile index shard document for %s", modPath)
		}
		if err = util.WriteFileAtomic(shardFilename(modPath), docBytes); err != nil {
			return false, ex.Wrapf(err, "writing compile index shard for %s", modPath)
		}
	}
	return buildUsesIgnore, nil
}

// Decode once during setup so routing and digests describe the same valid
// bytes, even if an artifact was changed between its original write and Store.
func setsFromArtifacts(matched, names []byte) ([]*rule.InstRuleSet, error) {
	var sets []*rule.InstRuleSet
	if err := json.Unmarshal(matched, &sets); err != nil {
		return nil, ex.Wrapf(err, "validating matched rules for compile index")
	}
	var resolvedNames map[string]string
	if err := json.Unmarshal(names, &resolvedNames); err != nil {
		return nil, ex.Wrapf(err, "validating import names for compile index")
	}
	for _, set := range sets {
		if set == nil {
			return nil, ex.New("null rule set in compile index input")
		}
	}
	return sets, nil
}

func packagesFromSets(sets []*rule.InstRuleSet) ([]string, error) {
	packages := make([]string, 0, len(sets))
	for _, set := range sets {
		if set == nil {
			return nil, ex.New("null rule set in compile index input")
		}
		packages = append(packages, set.ModulePath)
	}
	slices.Sort(packages)
	return slices.Compact(packages), nil
}

// LookupResult is the verified compile-index outcome for one package.
type LookupResult struct {
	Sets                     []*rule.InstRuleSet
	BuildUsesIgnoreDirective bool
	Excluded                 bool
}

// Excludes reports whether the verified compile index excludes importPath.
func Excludes(importPath string) bool {
	res, ok := Lookup(importPath)
	return ok && res.Excluded
}

// Lookup checks the compile index for importPath. If the index is valid and
// excludes importPath, Excluded=true and ok=true are returned. If importPath is
// a candidate and has a valid shard, its rule sets, BuildUsesIgnoreDirective,
// and ok=true are returned.
//
// Any missing, unreadable, stale, or corrupt state returns ok=false so callers
// take the existing full loader path, preserving its diagnostics. Hashing both
// setup artifacts avoids suppressing errors in a changed or corrupt file; no
// package decision depends on the spelling or formatting of JSON bytes.
func Lookup(importPath string) (LookupResult, bool) {
	if importPath == "" {
		return LookupResult{}, false
	}
	idx, valid := readVerifiedIndex()
	if !valid {
		return LookupResult{}, false
	}
	matched, err := os.ReadFile(util.GetMatchedRuleFile())
	if err != nil || sha256.Sum256(matched) != idx.MatchedHash {
		return LookupResult{}, false
	}
	if !slices.Contains(idx.Packages, importPath) {
		names, namesErr := os.ReadFile(util.GetImportNamesFile())
		if namesErr != nil || sha256.Sum256(names) != idx.NamesHash {
			return LookupResult{}, false
		}
		return LookupResult{Excluded: true}, true
	}
	shardBytes, err := os.ReadFile(shardFilename(importPath))
	if err != nil {
		return LookupResult{}, false
	}
	var shard shardDocument
	if json.Unmarshal(shardBytes, &shard) != nil ||
		shard.MatchedHash != idx.MatchedHash ||
		sha256.Sum256(shard.Sets) != shard.Digest {
		return LookupResult{}, false
	}
	var sets []*rule.InstRuleSet
	if json.Unmarshal(shard.Sets, &sets) != nil {
		return LookupResult{}, false
	}
	for _, set := range sets {
		if set == nil || set.ModulePath != importPath {
			return LookupResult{}, false
		}
	}
	return LookupResult{
		Sets:                     sets,
		BuildUsesIgnoreDirective: idx.BuildUsesIgnoreDirective,
	}, true
}

func readVerifiedIndex() (index, bool) {
	data, err := os.ReadFile(filename())
	if err != nil {
		return index{}, false
	}
	var doc document
	if json.Unmarshal(data, &doc) != nil || sha256.Sum256(doc.Index) != doc.Digest {
		return index{}, false
	}
	var idx index
	if json.Unmarshal(doc.Index, &idx) != nil || idx.Version != version {
		return index{}, false
	}
	return idx, true
}
