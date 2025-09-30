// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package data

import (
	"embed"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

//go:embed *
var dataFs embed.FS

// ListEmbedFiles lists all the files in the embedded data
func ListEmbedFiles() ([]string, error) {
	rules, err := dataFs.ReadDir(".")
	if err != nil {
		return nil, ex.Wrapf(err, "failed to read directory")
	}

	var ruleFiles []string
	for _, rule := range rules {
		if !rule.IsDir() && strings.HasSuffix(rule.Name(), ".yaml") {
			ruleFiles = append(ruleFiles, rule.Name())
		}
	}
	return ruleFiles, nil
}

// ReadEmbedFile reads a file from the embedded data
func ReadEmbedFile(path string) ([]byte, error) {
	bs, err := dataFs.ReadFile(path)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to read file")
	}
	return bs, nil
}
