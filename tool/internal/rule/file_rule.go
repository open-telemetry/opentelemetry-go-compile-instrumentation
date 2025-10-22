// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

// InstFileRule represents a rule that allows adding a new file to the target
// package. For example, if we want to add a new file to the target package,
// we can define a rule:
//
//	rule:
//		name: "newrule"
//		target: "main"
//		file: "newfile.go"
//		path: "github.com/foo/bar/newfile"
type InstFileRule struct {
	InstBaseRule
	File string `json:"file" yaml:"file"` // The name of the file to be added to the target package
	Path string `json:"path" yaml:"path"` // The module path where the file is located
}
