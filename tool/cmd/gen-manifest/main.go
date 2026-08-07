// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otelc/tool/internal/manifest"
)

const generatedFilePerm = 0o644

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

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
	if err = writeFileAtomic("tool/data/instrumentation-manifest.json", content); err != nil {
		return fmt.Errorf("write instrumentation manifest: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, content []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".instrumentation-manifest-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmp != nil {
			if closeErr := tmp.Close(); err == nil {
				err = closeErr
			}
		}
		if tmpPath == "" {
			return
		}
		if removeErr := os.Remove(tmpPath); err == nil && !os.IsNotExist(removeErr) {
			err = removeErr
		}
	}()

	if err = tmp.Chmod(generatedFilePerm); err != nil {
		return err
	}
	if _, err = tmp.Write(content); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	tmp = nil
	if err = os.Rename(tmpPath, path); err != nil {
		return err
	}
	tmpPath = ""
	return nil
}
