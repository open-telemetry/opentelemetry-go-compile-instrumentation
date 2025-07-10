// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package data

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed *.yaml
var ruleFs embed.FS

func ListAvailableRules() ([]string, error) {
	rules, err := ruleFs.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var ruleFiles []string
	for _, rule := range rules {
		if !rule.IsDir() && strings.HasSuffix(rule.Name(), ".yaml") {
			ruleFiles = append(ruleFiles, rule.Name())
		}
	}
	return ruleFiles, nil
}
