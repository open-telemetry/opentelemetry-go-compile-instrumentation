// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"encoding/json"
	"os"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// load loads the matched rules from the build temp directory.
// TODO: Shared memory across all sub-processes is possible
func (ip *InstrumentPhase) load() ([]*rule.InstRule, error) {
	f := util.GetMatchedRuleFile()
	content, err := os.ReadFile(f)
	if err != nil {
		return nil, ex.Errorf(err, "failed to read file %s", f)
	}
	if len(content) == 0 {
		return nil, nil
	}

	rules := make([]*rule.InstRule, 0)
	err = json.Unmarshal(content, &rules)
	if err != nil {
		return nil, ex.Errorf(err, "failed to unmarshal rules from file %s", f)
	}
	ip.Debug("Loaded matched rules", "rules", rules)
	return rules, nil
}
