// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"encoding/json"
	"os"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// load loads the matched rules from the build temp directory.
// TODO: Shared memory across all sub-processes is possible
func (ip *InstrumentPhase) load() ([]*rule.InstRuleSet, error) {
	f := util.GetMatchedRuleFile()
	content, err := os.ReadFile(f)
	if os.IsNotExist(err) {
		return nil, ex.Newf("no instrumentation configuration found (%s does not exist); "+
			"run `otelc setup` in the module directory before building with `-toolexec`, "+
			"or build with `otelc go build` instead", f)
	}
	if err != nil {
		return nil, ex.Wrapf(err, "failed to read file %s", f)
	}
	rset := make([]*rule.InstRuleSet, 0)
	err = json.Unmarshal(content, &rset)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to unmarshal JSON")
	}

	ip.Debug("Load matched rule sets", "path", f)
	return rset, nil
}

// match matches the rules with the compile command.
func (ip *InstrumentPhase) match(allSet []*rule.InstRuleSet, args []string) *rule.InstRuleSet {
	// One package can only be matched with one rule set, so it's safe to return
	// the first matched rule set.
	importPath := util.FindFlagValue(args, "-p")
	util.Assert(importPath != "", "sanity check")
	for _, rset := range allSet {
		if rset.ModulePath == importPath {
			ip.Debug("Match rule set", "set", rset)
			return rset
		}
	}
	return nil
}
