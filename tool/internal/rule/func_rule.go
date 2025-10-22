// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

// InstFuncRule represents a rule that guides hook function injection into
// appropriate target function locations. For example, if we want to inject
// custom Foo function at the entry of target function Bar, we can define a rule:
//
//	rule:
//		name: "newrule"
//		target: "main"
//		func: "Bar"
//		recv: "*RecvType"
//		before: "Foo"
//		path: "github.com/foo/bar/hook_rule"
type InstFuncRule struct {
	InstBaseRule
	Func   string `json:"func"   yaml:"func"`   // The name of the target func to be instrumented
	Recv   string `json:"recv"   yaml:"recv"`   // The name of the receiver type
	Before string `json:"before" yaml:"before"` // The function we inject at the target function entry
	After  string `json:"after"  yaml:"after"`  // The function we inject at the target function exit
	Path   string `json:"path"   yaml:"path"`   // The module path where hook code is located
}
