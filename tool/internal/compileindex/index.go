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

// Store publishes the index after setup has successfully stored both artifacts.
// A concurrent reader of an older index falls back if either artifact changes.
func Store(packages []string) error {
	matched, err := os.ReadFile(util.GetMatchedRuleFile())
	if err != nil {
		return ex.Wrapf(err, "reading matched rules for compile index")
	}
	names, err := os.ReadFile(util.GetImportNamesFile())
	if err != nil {
		return ex.Wrapf(err, "reading import names for compile index")
	}
	packages = slices.Clone(packages)
	slices.Sort(packages)
	data, err := json.Marshal(index{
		Version:     version,
		Packages:    slices.Compact(packages),
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
