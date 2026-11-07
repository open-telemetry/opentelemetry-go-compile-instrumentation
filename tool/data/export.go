// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package data

import (
	"bytes"
	_ "embed"
)

//go:embed otelc-bundle.tgz
var bundle []byte

//go:embed instrumentation-manifest.json
var manifestJSON []byte

// GetBundleReader returns a bytes.Reader for the embedded bundle
func GetBundleReader() *bytes.Reader {
	return bytes.NewReader(bundle)
}

// GetManifestJSON returns a copy of the embedded instrumentation-manifest.json.
func GetManifestJSON() []byte {
	return bytes.Clone(manifestJSON)
}
