// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func (ip *instrumentPhase) reportDecision(r rule.InstRule, reason string) {
	err := rule.WriteMatchDecision(ip.debugArtifactDir(), rule.MatchDecision{
		Phase:   "instrument",
		Package: util.FindFlagValue(ip.compileArgs, "-p"),
		Source:  ip.diagnosticSource,
		Rule:    r,
		Reason:  reason,
	})
	if err != nil {
		ip.Warn("failed to write rule diagnostics", "error", err)
	}
}
