// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package compileindex stores an optional setup-produced routing index. A
// negative is trusted only while both setup artifacts still match its digests.
package compileindex

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"slices"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const version = 1

// index is derived data, not a replacement for the matched rule sets. In
// particular, candidate compiles still use the existing file/test/cgo matcher.
type index struct {
	Version     int      `json:"version"`
	Packages    []string `json:"packages"`
	MatchedHash [32]byte `json:"matched_sha256"`
	NamesHash   [32]byte `json:"names_sha256"`
}

// document detects corruption of the routing metadata itself, including a
// valid-JSON edit that removes a package but leaves artifact hashes unchanged.
type document struct {
	Index  json.RawMessage `json:"index"`
	Digest [32]byte        `json:"sha256"`
}

func filename() string { return util.GetBuildTemp("compile_index.json") }

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
	packages, err := packagesFromArtifacts(matched, names)
	if err != nil {
		return err
	}
	return writeIndex(matched, names, packages)
}

// StoreFromArtifacts publishes the index from already-marshaled setup
// artifacts and rule sets, avoiding a redundant read and JSON decode.
func StoreFromArtifacts(matched, names []byte, sets []*rule.InstRuleSet) error {
	packages, err := packagesFromSets(sets)
	if err != nil {
		return err
	}
	return writeIndex(matched, names, packages)
}

func writeIndex(matched, names []byte, packages []string) error {
	data, err := json.Marshal(index{
		Version:     version,
		Packages:    packages,
		MatchedHash: sha256.Sum256(matched),
		NamesHash:   sha256.Sum256(names),
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

// Decode once during setup so routing and digests describe the same valid
// bytes, even if an artifact was changed between its original write and Store.
func packagesFromArtifacts(matched, names []byte) ([]string, error) {
	var sets []*rule.InstRuleSet
	if err := json.Unmarshal(matched, &sets); err != nil {
		return nil, ex.Wrapf(err, "validating matched rules for compile index")
	}
	var resolvedNames map[string]string
	if err := json.Unmarshal(names, &resolvedNames); err != nil {
		return nil, ex.Wrapf(err, "validating import names for compile index")
	}
	return packagesFromSets(sets)
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

// Excludes reports a verified negative. Missing, unreadable, stale or unknown
// index data always takes the existing loader path, preserving its diagnostics.
// Hashing both artifacts avoids suppressing errors in a changed/corrupt file;
// no package decision depends on the spelling or formatting of JSON bytes.
func Excludes(importPath string) bool {
	if importPath == "" {
		return false
	}
	data, err := os.ReadFile(filename())
	if err != nil {
		return false
	}
	var doc document
	if json.Unmarshal(data, &doc) != nil || sha256.Sum256(doc.Index) != doc.Digest {
		return false
	}
	var idx index
	if json.Unmarshal(doc.Index, &idx) != nil || idx.Version != version {
		return false
	}
	if slices.Contains(idx.Packages, importPath) {
		return false
	}
	matched, err := os.ReadFile(util.GetMatchedRuleFile())
	if err != nil || sha256.Sum256(matched) != idx.MatchedHash {
		return false
	}
	names, err := os.ReadFile(util.GetImportNamesFile())
	return err == nil && sha256.Sum256(names) == idx.NamesHash
}
