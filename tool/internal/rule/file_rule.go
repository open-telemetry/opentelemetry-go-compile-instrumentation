// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"path/filepath"
	"slices"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.yaml.in/yaml/v3"
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

	File         string `json:"file"          yaml:"file"`
	Path         string `json:"path"          yaml:"path"`
	ResolvedPath string `json:"resolved_path" yaml:"-"` // local dir Path resolves to
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
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid file rule %q", name)
	}
	return &r, nil
}

// IsDisallowedFilePath reports whether p is an absolute, volume-relative,
// or otherwise rooted path that cannot represent a package-relative file.
func IsDisallowedFilePath(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return true
	}
	if filepath.VolumeName(p) != "" {
		return true
	}
	// Check for Windows drive prefix (e.g. C:foo or C:\foo) portably on all platforms.
	if len(p) >= 2 && (('a' <= p[0] && p[0] <= 'z') || ('A' <= p[0] && p[0] <= 'Z')) && p[1] == ':' {
		return true
	}
	return false
}

// hasParentTraversal reports whether p attempts to traverse out of its parent
// directory via ".." path segments.
func hasParentTraversal(p string) bool {
	parts := strings.FieldsFunc(p, func(c rune) bool { return c == '/' || c == '\\' })
	if slices.Contains(parts, "..") {
		return true
	}
	clean := filepath.Clean(filepath.FromSlash(p))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return true
	}
	return false
}

func (r *InstFileRule) validate() error {
	if strings.TrimSpace(r.File) == "" {
		return ex.Newf("file cannot be empty")
	}
	if strings.TrimSpace(r.Path) == "" {
		return ex.Newf("path cannot be empty")
	}
	clean := filepath.Clean(filepath.FromSlash(r.File))
	if clean == "." {
		return ex.Newf("file %q must refer to a file, not current directory", r.File)
	}
	if IsDisallowedFilePath(r.File) {
		return ex.Newf("file %q must be a relative path without drive or root specifiers", r.File)
	}
	if hasParentTraversal(r.File) {
		return ex.Newf("file %q cannot contain parent-directory traversal", r.File)
	}
	return nil
}
