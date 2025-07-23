// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package data

import (
	"embed"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

//go:embed *.yaml
var dataFs embed.FS

func ListAvailableRules() ([]string, error) {
	rules, err := dataFs.ReadDir(".")
	if err != nil {
		return nil, ex.Errorf(err, "failed to read directory")
	}

	var ruleFiles []string
	for _, rule := range rules {
		if !rule.IsDir() && strings.HasSuffix(rule.Name(), ".yaml") {
			ruleFiles = append(ruleFiles, rule.Name())
		}
	}
	return ruleFiles, nil
}

func ReadEmbedFile(path string) ([]byte, error) {
	bs, err := dataFs.ReadFile(path)
	if err != nil {
		return nil, ex.Errorf(err, "failed to read file")
	}
	return bs, nil
}
