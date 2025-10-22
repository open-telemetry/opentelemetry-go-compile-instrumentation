// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

// InstRawRule represents a rule that allows raw Go source code injection into
// appropriate target function locations. For example, if we want to inject
// raw code at the entry of target function Bar, we can define a rule:
//
//	rule:
//		name: "newrule"
//		target: "main"
//		func: "Bar"
//		raw: "println(\"Hello, World!\")"
type InstRawRule struct {
	InstBaseRule
	Func string `json:"func" yaml:"func"` // The name of the target func to be instrumented
	Raw  string `json:"raw"  yaml:"raw"`  // The raw code to be injected
}
