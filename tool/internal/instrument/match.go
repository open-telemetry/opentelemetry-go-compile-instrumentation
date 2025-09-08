// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// match matches the rules with the compile command.
func (ip *InstrumentPhase) match(args []string) ([]*rule.InstFuncRule, error) {
	util.Assert(util.IsCompileCommand(strings.Join(args, " ")), "sanity check")
	// Load matched hook rules from setup phase
	rules, err := ip.load()
	if err != nil {
		return nil, err
	}

	ip.Debug("Matching rules", "args", args, "rules", rules)

	// Check if the package is in the rules.
	importPath := util.FindFlagValue(args, "-p")
	util.Assert(importPath != "", "sanity check")
	matchedRules := make([]*rule.InstFuncRule, 0)
	for _, rule := range rules {
		if rule.GetFuncImportPath() == importPath {
			matchedRules = append(matchedRules, rule)
		}
	}
	ip.Debug("Matched rules", "matchedRules", matchedRules)
	return matchedRules, nil
}
