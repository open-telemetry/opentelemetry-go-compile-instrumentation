// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bufio"
	"bytes"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"go.opentelemetry.io/otelc/tool/ex"
)

// This file reads, writes and merges vendor/modules.txt files. The format is
// the one that `go mod vendor` writes:
//
//	# example.com/a v1.0.0 => ../a     module line (the replacement is optional)
//	## explicit; go 1.25               annotations (optional)
//	example.com/a                      one line for each vendored package
//	example.com/a/sub
//	# example.com/z => ./z             replacement-only line (at the end)

const (
	modulesTxtModulePrefix     = "# "
	modulesTxtAnnotationPrefix = "## "
	modulesTxtReplaceArrow     = "=>"
	annotationExplicit         = "explicit"
	annotationGoPrefix         = "go "
	// annotationBundlePrefix starts the annotation that otelc adds to the
	// otelc modules that it puts in vendor/. Its value is the fingerprint of
	// the otelc bundle (see bundleFingerprint). The go command ignores
	// annotations that it does not know.
	annotationBundlePrefix = "otelc-bundle "
)

// vendorModule is one module block of a vendor/modules.txt file.
type vendorModule struct {
	// Path is the module path.
	Path string
	// Version is the module version. It is empty for a replacement-only line
	// that has no version (a wildcard replacement).
	Version string
	// Replacement is the text after "=>", for example "../x" or
	// "example.com/y v1.2.0". It is empty when the module is not replaced.
	Replacement string
	// Annotations are the items of the "## " line, in their order.
	Annotations []string
	// Packages are the vendored packages of the module, in file order.
	Packages []string
}

// trailing reports whether m is a replacement-only entry. `go mod vendor`
// writes these entries at the end of the file. They have no annotations and
// no packages: they record replacements that the build does not use.
func (m *vendorModule) trailing() bool {
	return m.Version == "" || (len(m.Annotations) == 0 && len(m.Packages) == 0)
}

func (m *vendorModule) explicit() bool {
	return slices.Contains(m.Annotations, annotationExplicit)
}

// goVersion returns the version of the "go" annotation, or "".
func (m *vendorModule) goVersion() string {
	return m.annotation(annotationGoPrefix)
}

// annotation returns the value of the annotation that starts with prefix, or
// "".
func (m *vendorModule) annotation(prefix string) string {
	for _, a := range m.Annotations {
		if v, ok := strings.CutPrefix(a, prefix); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// setAnnotation sets the annotation that starts with prefix to prefix+value.
func (m *vendorModule) setAnnotation(prefix, value string) {
	m.Annotations = slices.DeleteFunc(m.Annotations, func(a string) bool {
		return strings.HasPrefix(a, prefix)
	})
	m.Annotations = append(m.Annotations, prefix+value)
}

func (m *vendorModule) clone() *vendorModule {
	c := *m
	c.Annotations = slices.Clone(m.Annotations)
	c.Packages = slices.Clone(m.Packages)
	return &c
}

// modulesTxt is a parsed vendor/modules.txt file.
type modulesTxt struct {
	modules []*vendorModule
}

// parseModulesTxt parses the content of a vendor/modules.txt file.
func parseModulesTxt(data []byte) (*modulesTxt, error) {
	m := &modulesTxt{}
	var current *vendorModule
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// Package paths are short, but a file can have many lines. Permit long
	// lines to be safe.
	const maxLine = 1024 * 1024
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxLine)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimRight(scanner.Text(), "\r")
		switch {
		case strings.TrimSpace(line) == "":
			continue
		case strings.HasPrefix(line, modulesTxtAnnotationPrefix):
			if current == nil {
				return nil, ex.Newf("vendor/modules.txt line %d: annotation before any module line", lineNo)
			}
			for item := range strings.SplitSeq(strings.TrimPrefix(line, modulesTxtAnnotationPrefix), ";") {
				if item = strings.TrimSpace(item); item != "" {
					current.Annotations = append(current.Annotations, item)
				}
			}
		case strings.HasPrefix(line, modulesTxtModulePrefix):
			mod, err := parseModuleLine(strings.TrimPrefix(line, modulesTxtModulePrefix))
			if err != nil {
				return nil, ex.Wrapf(err, "vendor/modules.txt line %d", lineNo)
			}
			m.modules = append(m.modules, mod)
			current = mod
		case strings.HasPrefix(line, "#"):
			return nil, ex.Newf("vendor/modules.txt line %d: unknown line %q", lineNo, line)
		default:
			if current == nil {
				return nil, ex.Newf("vendor/modules.txt line %d: package line before any module line", lineNo)
			}
			pkg := strings.TrimSpace(line)
			if err := checkVendoredPackage(current.Path, pkg); err != nil {
				return nil, ex.Wrapf(err, "vendor/modules.txt line %d", lineNo)
			}
			current.Packages = append(current.Packages, pkg)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, ex.Wrapf(err, "reading vendor/modules.txt")
	}
	return m, nil
}

// parseModuleLine parses the text after "# " of a module line.
func parseModuleLine(text string) (*vendorModule, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil, ex.New("module line has no module path")
	}
	mod := &vendorModule{Path: fields[0]}
	// otelc makes file paths from the module paths and the package paths, so
	// a path such as "../x" must not get in.
	if err := module.CheckImportPath(mod.Path); err != nil {
		return nil, ex.Wrapf(err, "module line has an invalid module path %q", mod.Path)
	}
	rest := fields[1:]
	if len(rest) == 0 {
		return nil, ex.Newf("module line of %s has no version", mod.Path)
	}
	if rest[0] != modulesTxtReplaceArrow {
		if !semver.IsValid(rest[0]) {
			return nil, ex.Newf("module line of %s has an invalid version %q", mod.Path, rest[0])
		}
		mod.Version = rest[0]
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return mod, nil
	}
	if rest[0] != modulesTxtReplaceArrow {
		return nil, ex.Newf("module line of %s has unexpected text %q", mod.Path, strings.Join(rest, " "))
	}
	if len(rest) == 1 {
		return nil, ex.Newf("module line of %s has no replacement after %q", mod.Path, modulesTxtReplaceArrow)
	}
	mod.Replacement = strings.Join(rest[1:], " ")
	return mod, nil
}

// checkVendoredPackage returns an error when pkg is not a valid import path in
// the module modPath: the package must be the module path or below it.
func checkVendoredPackage(modPath, pkg string) error {
	if err := module.CheckImportPath(pkg); err != nil {
		return ex.Wrapf(err, "invalid package path %q", pkg)
	}
	if pkg != modPath && !strings.HasPrefix(pkg, modPath+"/") {
		return ex.Newf("package %s is not in its module %s", pkg, modPath)
	}
	return nil
}

// Format returns the content of the file, in the format of `go mod vendor`.
func (m *modulesTxt) Format() []byte {
	lines := make([]string, 0, len(m.modules))
	for _, mod := range m.modules {
		line := modulesTxtModulePrefix + mod.Path
		if mod.Version != "" {
			line += " " + mod.Version
		}
		if mod.Replacement != "" {
			line += " " + modulesTxtReplaceArrow + " " + mod.Replacement
		}
		lines = append(lines, line)
		if len(mod.Annotations) > 0 {
			lines = append(lines, modulesTxtAnnotationPrefix+strings.Join(mod.Annotations, "; "))
		}
		lines = append(lines, mod.Packages...)
	}
	if len(lines) == 0 {
		return []byte{}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// find returns the module block of path that the build uses, or nil. A
// replacement-only entry is not used by the build, so it is not found.
func (m *modulesTxt) find(path string) *vendorModule {
	for _, mod := range m.modules {
		if mod.Path == path && !mod.trailing() {
			return mod
		}
	}
	return nil
}

// Module returns the vendored version of the module path.
func (m *modulesTxt) Module(path string) (string, bool) {
	if mod := m.find(path); mod != nil {
		return mod.Version, true
	}
	return "", false
}

// Explicit reports whether the module path has the "## explicit" annotation.
func (m *modulesTxt) Explicit(path string) bool {
	mod := m.find(path)
	return mod != nil && mod.explicit()
}

// Packages returns the sorted list of the vendored packages of the module.
func (m *modulesTxt) Packages(path string) []string {
	mod := m.find(path)
	if mod == nil {
		return nil
	}
	pkgs := slices.Clone(mod.Packages)
	slices.Sort(pkgs)
	return pkgs
}

// hasPackage reports whether the package importPath is vendored.
func (m *modulesTxt) hasPackage(importPath string) bool {
	return m.moduleOfPackage(importPath) != nil
}

// moduleOfPackage returns the module block that vendors the package
// importPath, or nil.
func (m *modulesTxt) moduleOfPackage(importPath string) *vendorModule {
	for _, mod := range m.modules {
		if !mod.trailing() && slices.Contains(mod.Packages, importPath) {
			return mod
		}
	}
	return nil
}

// usedModules returns the module blocks that the build uses (not the
// replacement-only entries).
func (m *modulesTxt) usedModules() []*vendorModule {
	out := make([]*vendorModule, 0, len(m.modules))
	for _, mod := range m.modules {
		if !mod.trailing() {
			out = append(out, mod)
		}
	}
	return out
}

// without returns a copy of m without the module blocks of paths that the
// build uses. The replacement-only entries stay.
func (m *modulesTxt) without(paths map[string]bool) *modulesTxt {
	c := m.clone()
	c.modules = slices.DeleteFunc(c.modules, func(mod *vendorModule) bool {
		return paths[mod.Path] && !mod.trailing()
	})
	return c
}

func (m *modulesTxt) clone() *modulesTxt {
	c := &modulesTxt{modules: make([]*vendorModule, 0, len(m.modules))}
	for _, mod := range m.modules {
		c.modules = append(c.modules, mod.clone())
	}
	return c
}

// vendorConflictError tells that the hook code of otelc needs a module
// version that the vendor directory of the user does not have. A build never
// changes vendored code, so it cannot continue. `otelc vendor` can change
// vendor/ (it gets the higher version, like "go get").
type vendorConflictError struct {
	// Module is the module path.
	Module string
	// VendoredVersion is the version in vendor/modules.txt.
	VendoredVersion string
	// RequiredVersion is the version that the hook code needs.
	RequiredVersion string
	// Package is set when the vendored version is higher, but it does not have
	// a package that the hook code needs.
	Package string
	// RequiredBy is the module that needs RequiredVersion, when it is known.
	RequiredBy string
}

func (e *vendorConflictError) Error() string {
	if e.Package != "" {
		return fmt.Sprintf(
			"vendor conflict: the otelc hook code needs package %s of module %s "+
				"(resolved at version %s), but vendor/ has %s %s without this package. "+
				"otelc does not change vendored code. To fix it, make your module import %s "+
				"(for example with a blank import), run \"go mod vendor\", and build again",
			e.Package, e.Module, e.RequiredVersion, e.Module, e.VendoredVersion, e.Package)
	}
	requiredBy := ""
	if e.RequiredBy != "" {
		requiredBy = " (required by " + e.RequiredBy + ")"
	}
	return fmt.Sprintf(
		"vendor conflict: the otelc hook code needs module %s at version %s or higher%s, "+
			"but vendor/ has version %s. A build does not change vendored code. "+
			"To fix it, run \"go get %s@%s\" and \"go mod vendor\", "+
			"or run \"otelc vendor\" to update vendor/ (this needs network access), and build again",
		e.Module, e.RequiredVersion, requiredBy, e.VendoredVersion, e.Module, e.RequiredVersion)
}

// mergeVendorModules adds the modules and packages of injected (the modules
// that the hook code needs) to user (the vendor/modules.txt of the user). It
// returns the merged file and the sorted import paths of the packages that
// the user does not vendor yet. user is not changed.
//
// The rules follow minimal version selection, but otelc never changes the
// vendored code of the user:
//   - A module that the user does not vendor is added. It is marked explicit,
//     because otelc adds a require for it to go.mod.
//   - A module at the same version gets the packages that it does not have
//     yet.
//   - A module that the user vendors at a higher version is kept. If the hook
//     code needs a package that the user does not vendor, this is a conflict:
//     one module cannot come from two versions.
//   - A module that the user vendors at a lower version is a conflict.
//
// Replacement-only entries of injected are not added: they are replacements of
// the scratch module, not of the user.
func mergeVendorModules(user, injected *modulesTxt) (*modulesTxt, []string, error) {
	merged := user.clone()
	var added []string
	changed := false

	injectedMods := injected.usedModules()
	slices.SortStableFunc(injectedMods, func(a, b *vendorModule) int {
		return strings.Compare(a.Path, b.Path)
	})

	for _, inj := range injectedMods {
		existing := merged.find(inj.Path)
		if existing == nil {
			mod := inj.clone()
			if !mod.explicit() {
				mod.Annotations = append([]string{annotationExplicit}, mod.Annotations...)
			}
			slices.Sort(mod.Packages)
			merged.modules = append(merged.modules, mod)
			added = append(added, mod.Packages...)
			changed = true
			continue
		}

		missing := make([]string, 0)
		for _, pkg := range inj.Packages {
			if !slices.Contains(existing.Packages, pkg) {
				missing = append(missing, pkg)
			}
		}
		slices.Sort(missing)

		switch cmp := semver.Compare(inj.Version, existing.Version); {
		case cmp > 0:
			return nil, nil, &vendorConflictError{
				Module:          inj.Path,
				VendoredVersion: existing.Version,
				RequiredVersion: inj.Version,
			}
		case cmp < 0:
			if len(missing) > 0 {
				return nil, nil, &vendorConflictError{
					Module:          inj.Path,
					VendoredVersion: existing.Version,
					RequiredVersion: inj.Version,
					Package:         missing[0],
				}
			}
		default:
			if len(missing) > 0 {
				existing.Packages = append(existing.Packages, missing...)
				slices.Sort(existing.Packages)
				added = append(added, missing...)
				changed = true
			}
		}
	}

	if !changed {
		return merged, nil, nil
	}

	// Sort the modules by path, like `go mod vendor` does. The
	// replacement-only entries of the user stay at the end, in their order.
	used := merged.usedModules()
	slices.SortStableFunc(used, func(a, b *vendorModule) int {
		return strings.Compare(a.Path, b.Path)
	})
	for _, mod := range merged.modules {
		if mod.trailing() {
			used = append(used, mod)
		}
	}
	merged.modules = used

	slices.Sort(added)
	return merged, added, nil
}
