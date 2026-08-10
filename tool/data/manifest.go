// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package data

import (
	"bytes"
	_ "embed"
)

//go:embed instrumentation-manifest.json
var manifestJSON []byte

func GetManifestJSON() []byte {
	return bytes.Clone(manifestJSON)
}
