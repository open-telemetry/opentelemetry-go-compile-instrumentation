// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
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

	File       string `json:"file" yaml:"file"`   // The name of the file to be added to the target package
	Path       string `json:"path" yaml:"path"`   // The import path where the file is located
	ModulePath string `json:"-"    yaml:"module"` // The module path where the file is located

	ResolvedPath string `json:"resolved_path" yaml:"-"` // The local path of the package directory resolved from import path
}

// NewInstFileRule loads and validates an InstFileRule from YAML data.
func NewInstFileRule(data []byte, name string) (*InstFileRule, error) {
	var r InstFileRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}
	if r.ModulePath == "" {
		r.ModulePath = r.Path
	}
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid file rule %q", name)
	}
	return &r, nil
}

func (r *InstFileRule) validate() error {
	if strings.TrimSpace(r.File) == "" {
		return ex.Newf("file cannot be empty")
	}
	if strings.TrimSpace(r.Path) == "" {
		return ex.Newf("path cannot be empty")
	}
	if r.Path != r.ModulePath && !strings.HasPrefix(r.Path, r.ModulePath+"/") {
		return ex.Newf("import path %q is not part of module path %q", r.Path, r.ModulePath)
	}
	return nil
}
