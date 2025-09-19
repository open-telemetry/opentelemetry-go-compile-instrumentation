// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"

func (ip *InstrumentPhase) instrument(rules []*rule.InstFuncRule, args []string) error {
	for _, rule := range rules {
		err := ip.applyFuncRule(rule, args)
		if err != nil {
			return err
		}
	}
	return nil
}
