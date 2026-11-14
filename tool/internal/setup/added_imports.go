// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/module"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// suppressHooksImport is the package toolexec imports into instrumented
// packages when the build uses //otelc:ignore.
const suppressHooksImport = "runtime"

// ruleSetAddedImports returns the imports toolexec can add to set's target
// package: every rule's declared imports, the imports of its add_file sources,
// and the //otelc:ignore support package when usesIgnore is set. The go
// command must see these before the build, or they are missing from the
// importcfg of the package. Call and literal rules only change files that
// import the package they match, so their imports carry that package in
// requires.
func (sp *setupPhase) ruleSetAddedImports(set *rule.InstRuleSet, usesIgnore bool) []ruleImport {
	seen := make(map[ruleImport]bool)
	add := func(imports map[string]string, requires string) {
		for _, path := range imports {
			if path == set.ModulePath || path == "unsafe" || path == "C" {
				continue
			}
			seen[ruleImport{path: path, requires: requires}] = true
		}
	}
	declared := func(r interface{ Base() *rule.InstBaseRule }) (map[string]string, string) {
		return r.Base().Imports, ""
	}
	addRuleImports(set.RawRules, add, func(r *rule.InstRawRule) (map[string]string, string) { return declared(r) })
	addRuleImports(set.FuncRules, add, func(r *rule.InstFuncRule) (map[string]string, string) { return declared(r) })
	addRuleImports(set.StructRules, add, func(r *rule.InstStructRule) (map[string]string, string) { return declared(r) })
	addRuleImports(set.DirectiveRules, add, func(r *rule.InstDirectiveRule) (map[string]string, string) {
		return declared(r)
	})
	addRuleImports(set.DeclRules, add, func(r *rule.InstDeclRule) (map[string]string, string) { return declared(r) })
	addRuleImports(set.CallRules, add, func(r *rule.InstCallRule) (map[string]string, string) {
		if r.MethodCall != "" {
			// A method call matches on its receiver type, not on the file's imports.
			return r.Imports, ""
		}
		return r.Imports, r.ImportPath
	})
	addRuleImports(set.LitRules, add, func(r *rule.InstLitRule) (map[string]string, string) {
		return r.Imports, r.ImportPath
	})
	for _, r := range set.FileRules {
		imps, err := sp.fileRuleSourceImports(r)
		if err != nil {
			sp.Warn("could not inspect file rule source imports", "rule", r.Name, "error", err)
			continue
		}
		for _, imp := range imps {
			add(map[string]string{"": imp}, "")
		}
	}
	if usesIgnore {
		add(map[string]string{"": suppressHooksImport}, "")
	}
	return slices.SortedFunc(maps.Keys(seen), func(a, b ruleImport) int {
		if c := strings.Compare(a.path, b.path); c != 0 {
			return c
		}
		return strings.Compare(a.requires, b.requires)
	})
}

// anyIgnoredCallFiles reports whether any set holds a //otelc:ignore call.
func anyIgnoredCallFiles(sets []*rule.InstRuleSet) bool {
	for _, set := range sets {
		if len(set.IgnoredCallFiles) > 0 {
			return true
		}
	}
	return false
}

// targetKind says how the go command is made to see a package's added imports.
type targetKind int

const (
	// kindInPlace packages get an extra file in their own directory.
	kindInPlace targetKind = iota
	// kindModCache packages live in the read-only module cache, which an
	// overlay may not touch: their module is replaced by a directory that
	// exists only in the overlay.
	kindModCache
	// kindRuntimeFile is a file-target build (command-line-arguments): only
	// the files on the command line are compiled, so the imports go into the
	// generated otelc.runtime.go that is already put there.
	kindRuntimeFile
)

// moduleRoot is a module in the module cache.
type moduleRoot struct {
	Path, Version, Dir string
}

// importTarget is a package and the imports rules add to it that it does not
// already have.
type importTarget struct {
	ImportPath string
	Name       string
	Dir        string
	Kind       targetKind
	Module     moduleRoot
	Imports    []string
}

// importTargets returns, for every package the matched rules add imports to,
// the imports the go command has to see. Each package's existing imports
// come from the files the go command compiles for it (InstRuleSet.Sources).
func (sp *setupPhase) importTargets(matched []*rule.InstRuleSet, goroot, gomodcache string) ([]*importTarget, error) {
	usesIgnore := anyIgnoredCallFiles(matched)
	byKey := make(map[string]*importTarget)
	for _, set := range matched {
		adds := sp.ruleSetAddedImports(set, usesIgnore)
		if len(adds) == 0 {
			continue
		}
		name, existing := parseSources(set.Sources)
		if name == "" {
			sp.Warn("cannot read the sources of an instrumented package", "package", set.ModulePath)
			continue
		}
		dir := filepath.Dir(set.Sources[0])
		var imports []string
		for _, imp := range adds {
			if existing[imp.path] || (imp.requires != "" && !existing[imp.requires]) {
				continue
			}
			imports = append(imports, imp.path)
		}
		if len(imports) == 0 {
			continue
		}
		kind, mod := classifyDir(dir, gomodcache)
		if set.ModulePath == pkgload.CommandLineArgumentsPackage {
			kind = kindRuntimeFile
		}
		if within(filepath.Join(goroot, "src"), dir) {
			for _, imp := range imports {
				if !isStdPath(imp) {
					return nil, ex.Newf("rules add import %q to standard library package %s, "+
						"but the standard library cannot import packages outside it", imp, set.ModulePath)
				}
			}
		}
		key := dir + "\x00" + name
		t, ok := byKey[key]
		if !ok {
			t = &importTarget{ImportPath: set.ModulePath, Name: name, Dir: dir, Kind: kind, Module: mod}
			byKey[key] = t
		}
		t.Imports = append(t.Imports, imports...)
	}
	targets := make([]*importTarget, 0, len(byKey))
	for _, key := range slices.Sorted(maps.Keys(byKey)) {
		t := byKey[key]
		slices.Sort(t.Imports)
		t.Imports = slices.Compact(t.Imports)
		targets = append(targets, t)
	}
	return targets, nil
}

// parseSources returns the package name and imports of the readable Go files
// among sources. Build plan sources can name files the go command generates
// later (cgo); those are skipped.
func parseSources(sources []string) (string, map[string]bool) {
	name := ""
	imports := make(map[string]bool)
	fset := token.NewFileSet()
	for _, src := range sources {
		if !strings.HasSuffix(src, ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, src, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		if name == "" {
			name = f.Name.Name
		}
		for _, imp := range f.Imports {
			if path, err := strconv.Unquote(imp.Path.Value); err == nil {
				imports[path] = true
			}
		}
	}
	return name, imports
}

// classifyDir reports whether dir is inside the module cache and, if so,
// which module holds it.
func classifyDir(dir, gomodcache string) (targetKind, moduleRoot) {
	if !within(gomodcache, dir) {
		return kindInPlace, moduleRoot{}
	}
	rel, _ := filepath.Rel(gomodcache, dir)
	segs := strings.Split(filepath.ToSlash(rel), "/")
	for i, seg := range segs {
		escPath, version, ok := strings.Cut(seg, "@")
		if !ok {
			continue
		}
		path, err := module.UnescapePath(strings.Join(append(slices.Clone(segs[:i]), escPath), "/"))
		if err != nil {
			return kindInPlace, moduleRoot{}
		}
		return kindModCache, moduleRoot{
			Path:    path,
			Version: version,
			Dir:     filepath.Join(gomodcache, filepath.FromSlash(strings.Join(segs[:i+1], "/"))),
		}
	}
	return kindInPlace, moduleRoot{}
}

// within reports whether path is root or below it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isStdPath reports whether importPath names a standard library package: its
// first element has no dot, the rule the go command applies.
func isStdPath(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}
