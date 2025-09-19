// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"encoding/json"
	"os"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// store stores the matched rules to the file
// It's the pair of the InstrumentPhase.load
func (sp *SetupPhase) store(matched []*rule.InstFuncRule) error {
	f := util.GetMatchedRuleFile()
	file, err := os.Create(f)
	if err != nil {
		return ex.Errorf(err, "failed to create file %s", f)
	}
	defer file.Close()

	bs, err := json.Marshal(matched)
	if err != nil {
		return ex.Errorf(err, "failed to marshal rules to JSON")
	}

	_, err = file.Write(bs)
	if err != nil {
		return ex.Errorf(err, "failed to write JSON to file %s", f)
	}
	sp.Info("Stored matched rules", "rules", matched)
	return nil
}
