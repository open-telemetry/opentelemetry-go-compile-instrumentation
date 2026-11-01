// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package data

import (
	"bytes"
	_ "embed"
)

//go:embed instrumentation-manifest.json
var manifestJSON []byte

// GetManifestJSON returns a copy of the embedded instrumentation-manifest.json.
func GetManifestJSON() []byte {
	return bytes.Clone(manifestJSON)
}
