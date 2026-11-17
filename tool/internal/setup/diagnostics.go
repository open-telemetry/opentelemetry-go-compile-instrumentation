// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"sync"

	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func (sp *setupPhase) reportDecision(source string, r rule.InstRule, reason string) {
	if sp.decision != nil {
		sp.decision(source, r, reason)
	}
}

func (sp *setupPhase) withMatchDiagnostics(dep *Dependency) *setupPhase {
	if instrument.DiffDebugEnabled() {
		local := *sp
		if local.decisionMu == nil {
			local.decisionMu = &sync.Mutex{}
		}
		local.decision = func(source string, r rule.InstRule, reason string) {
			local.decisionMu.Lock()
			defer local.decisionMu.Unlock()
			err := rule.WriteMatchDecision(setupDebugDir(dep.ImportPath), rule.MatchDecision{
				Phase:   "setup",
				Package: dep.ImportPath,
				Source:  source,
				Rule:    r,
				Reason:  reason,
			})
			if err != nil {
				sp.Warn("failed to write rule diagnostics", "error", err)
			}
		}
		sp = &local
	}
	return sp
}
