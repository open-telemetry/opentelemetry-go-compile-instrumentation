// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"

	"go.opentelemetry.io/otelc/tool/data"
	"go.opentelemetry.io/otelc/tool/ex"
)

// Entry describes an instrumentation module's target and version range.
type Entry struct {
	ModulePath   string `json:"modulePath"`
	Target       string `json:"target"`
	VersionRange string `json:"versionRange,omitempty"`
}

// Manifest contains the built-in instrumentation metadata used during pinning.
type Manifest []Entry

// Load decodes the embedded instrumentation manifest.
func Load() (Manifest, error) {
	return load(data.GetManifestJSON())
}

func load(content []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, ex.Wrapf(err, "loading embedded instrumentation manifest")
	}
	return manifest, nil
}
