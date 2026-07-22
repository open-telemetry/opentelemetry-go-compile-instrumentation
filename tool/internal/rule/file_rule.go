// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"gopkg.in/yaml.v3"
)

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
	InstBaseRule `yaml:",inline"`

	File string `json:"file" yaml:"file"` // The name of the file to be added to the target package
	Path string `json:"path" yaml:"path"` // The import path where the file is located

	ResolvedPath string `json:"resolved_path" yaml:"-"` // The local path of the package directory resolved from import path
}

// NewInstFileRule builds an InstFileRule from the add_file modifier payload.
// File rules take no where selectors: the where.file key is a file predicate
// (routed to base.Where), not this rule's file field, so the where node is not
// decoded onto the rule here.
func NewInstFileRule(base InstBaseRule, _ *yaml.Node, act *FileAction) (*InstFileRule, error) {
	r := &InstFileRule{InstBaseRule: base}
	if act != nil {
		r.File, r.Path = act.File, act.Path
	}
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid file rule %q", base.Name)
	}
	return r, nil
}

func (r *InstFileRule) validate() error {
	if strings.TrimSpace(r.File) == "" {
		return ex.Newf("file cannot be empty")
	}
	if strings.TrimSpace(r.Path) == "" {
		return ex.Newf("path cannot be empty")
	}
	return nil
}
