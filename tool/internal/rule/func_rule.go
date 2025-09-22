// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import "strings"

// InstFuncRule represents a rule that guides hook function injection into
// appropriate target function locations. For example, if we want to inject
// custom Foo function at the entry of target function Bar, we can define a rule:
//
//	rule:
//		name: "rule"
//		path: "github.com/foo/bar/hook_rule"
//		pointcut: "Bar"
//		before: "Foo"
//
// The rule will be matched against the target function and the hook function
// will be injected at the appropriate location.
//
// The rule is defined in the yaml file, and the yaml file is embedded into the
// binary during the build process.
type InstFuncRule struct {
	// The unique name of the hook rule
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// The module path of the hook code
	Path string `json:"path"           yaml:"path"`
	// The full qualified name of the target function to be instrumented
	Pointcut string `json:"pointcut"       yaml:"pointcut"`
	// The function we inject at the target function entry
	Before string `json:"before"         yaml:"before"`
	// The function we inject at the target function exit
	After string `json:"after"          yaml:"after"`
}

func (r *InstFuncRule) String() string {
	return r.Name
}

func (r *InstFuncRule) GetPath() string {
	return r.Path
}

func (r *InstFuncRule) GetName() string {
	return r.Name
}

func (r *InstFuncRule) GetFuncName() string {
	return strings.Split(r.Pointcut, ".")[1]
}

func (r *InstFuncRule) GetFuncImportPath() string {
	return strings.Split(r.Pointcut, ".")[0]
}

func (r *InstFuncRule) GetBefore() string {
	return r.Before
}

func (r *InstFuncRule) GetAfter() string {
	return r.After
}
