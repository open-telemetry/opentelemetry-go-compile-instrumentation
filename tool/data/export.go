// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package data

import (
	"embed"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

//go:embed *
var dataFs embed.FS

// ReadEmbedFile reads a file from the embedded data
func ReadEmbedFile(path string) ([]byte, error) {
	bs, err := dataFs.ReadFile(path)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to read file")
	}
	return bs, nil
}
