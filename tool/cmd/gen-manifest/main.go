// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"go.opentelemetry.io/otelc/tool/internal/manifest"
	"go.opentelemetry.io/otelc/tool/util"
)

const generatedFilePerm = 0o644

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run expects the repository root as its working directory. The manifest
// source and output paths are both relative to it, as guaranteed by make.
func run() error {
	generated, err := manifest.Generate("instrumentation")
	if err != nil {
		return fmt.Errorf("generate instrumentation manifest: %w", err)
	}
	content, err := json.MarshalIndent(generated, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal instrumentation manifest: %w", err)
	}
	content = append(content, '\n')
	if err = util.WriteFileAtomic("tool/data/instrumentation-manifest.json", content, generatedFilePerm); err != nil {
		return fmt.Errorf("write instrumentation manifest: %w", err)
	}
	return nil
}
