// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package data

import (
	"embed"
	"strings"
)

//go:embed *.yaml
var ruleFs embed.FS

func UseDefaultRules() ([]string, error) {
	rules, err := ruleFs.ReadDir(".")
	if err != nil {
		return nil, err
	}

	var ruleFiles []string
	for _, rule := range rules {
		if !rule.IsDir() && strings.HasSuffix(rule.Name(), ".yaml") {
			ruleFiles = append(ruleFiles, rule.Name())
		}
	}
	return ruleFiles, nil
}
