// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
	"golang.org/x/tools/go/packages"
)

// resolveRulePaths resolves the import paths referenced by function and file rules
// to absolute filesystem paths.
func resolveRulePaths(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs []string) error {
	dirs := normalizeModuleDirs(moduleDirs)

	var pending []string
	for _, ruleset := range matched {
		for _, fileRule := range ruleset.FileRules {
			pending = append(pending, fileRule.Path)
		}
		for _, funcRule := range ruleset.AllFuncRules() {
			pending = append(pending, funcRule.Path)
		}
	}
	slices.Sort(pending)
	pending = slices.Compact(pending)

	resolved := make(map[string]string, len(pending))
	var lastErr error

	// findPackage returns the single package in pkgs matching importPath, nil if
	// none matched (the caller retries it against the next module dir), or an
	// error if importPath ambiguously matched more than one package.
	findPackage := func(pkgs []*packages.Package, importPath string) (*packages.Package, error) {
		var found *packages.Package
		for _, pkg := range pkgs {
			if pkg.PkgPath != importPath || len(pkg.Errors) > 0 || pkg.Dir == "" {
				continue
			}
			if found != nil {
				return nil, ex.Newf("import path %q resolved to multiple packages", importPath)
			}
			found = pkg
		}
		return found, nil
	}

	for _, moduleDir := range dirs {
		if len(pending) == 0 {
			break
		}

		pkgs, loadErr := packages.Load(&packages.Config{
			Mode:    packages.NeedName | packages.NeedFiles,
			Context: ctx,
			Dir:     moduleDir,
		}, pending...)
		if loadErr != nil {
			lastErr = loadErr
			continue
		}

		next := pending[:0]
		for _, p := range pending {
			pkg, err := findPackage(pkgs, p)
			switch {
			case err != nil:
				return err
			case pkg == nil:
				next = append(next, p)
			default:
				resolved[p] = pkg.Dir
			}
		}
		pending = next
	}

	if len(pending) > 0 {
		return ex.Wrapf(lastErr, "failed to resolve import path %q", pending[0])
	}

	for _, ruleset := range matched {
		for _, fileRule := range ruleset.FileRules {
			fileRule.ResolvedPath = resolved[fileRule.Path]
		}
		for _, funcRule := range ruleset.AllFuncRules() {
			funcRule.ResolvedPath = resolved[funcRule.Path]
		}
	}

	return nil
}

// ignoreDirectiveSupportModule is the hook package that provides the
// suppression functions (SuppressHooks/UnsuppressHooks/HooksSuppressed)
// every trampoline in the build calls into once any package uses
// //otelc:ignore. See ensureIgnoreDirectiveSupport.
const ignoreDirectiveSupportModule = util.OtelcInstRoot + "/runtime"

// ensureIgnoreDirectiveSupport fails fast, before any compiling starts, when
// the build uses //otelc:ignore but does not load instrumentation/runtime.
//
// A rule set's ModulePath is the target package the rules apply to (e.g.
// "runtime" for the standard library package instrumentation/runtime
// rewrites), not the module that supplies the rule's hook code, so it can't
// be used to tell whether instrumentation/runtime itself is part of this
// build. What can is the Path on the matched rules: any func or file rule
// contributed by instrumentation/runtime carries its import path there,
// since that's where the compiler looks up the hook or injected file it
// names. See #1550.
func ensureIgnoreDirectiveSupport(matched []*rule.InstRuleSet) error {
	usesIgnoreDirective := false
	for _, ruleset := range matched {
		if len(ruleset.IgnoredCallFiles) > 0 {
			usesIgnoreDirective = true
			break
		}
	}
	if !usesIgnoreDirective {
		return nil
	}

	for _, ruleset := range matched {
		for _, fileRule := range ruleset.FileRules {
			if fileRule.Path == ignoreDirectiveSupportModule {
				return nil
			}
		}
		for _, funcRule := range ruleset.AllFuncRules() {
			if funcRule.Path == ignoreDirectiveSupportModule {
				return nil
			}
		}
	}

	return ex.Newf(
		"//otelc:ignore is used in this build, but %s is not part of it; "+
			"add `_ %q` to otel.instrumentation.go so the trampolines %s guards "+
			"have the suppression support they need",
		ignoreDirectiveSupportModule, ignoreDirectiveSupportModule, util.DirectiveIgnore,
	)
}

// store stores the matched rules to the file.
// It's the pair of the InstrumentPhase.load.
// Rule paths must already be resolved via resolveRulePaths.
func (sp *setupPhase) store(matched []*rule.InstRuleSet) error {
	if err := ensureIgnoreDirectiveSupport(matched); err != nil {
		return err
	}

	bs, err := json.Marshal(matched)
	if err != nil {
		return ex.Wrapf(err, "failed to marshal rules to JSON")
	}

	f := util.GetMatchedRuleFile()
	err = util.WriteFileAtomic(f, bs)
	if err != nil {
		return ex.Wrapf(err, "failed to write matched rules to file %s", f)
	}
	sp.Info("Stored matched sets", "path", f)

	if err = sp.storeResolvedNames(); err != nil {
		return err
	}
	return sp.storePGOProfile()
}

// storePGOProfile records the PGO profile the build applies, for the nested
// builds toolexec runs. It always writes the file, empty when there is no
// profile, so a profile recorded by an earlier setup never applies to a later
// build.
func (sp *setupPhase) storePGOProfile() error {
	profile, ambiguous := pgoProfile(sp.pgo, sp.buildPackages)
	if ambiguous {
		sp.Warn("cannot pick a PGO profile for nested builds: main packages have different default.pgo files")
	}
	f := util.GetPGOProfileFile()
	if err := util.WriteFileAtomic(f, []byte(profile)); err != nil {
		return ex.Wrapf(err, "failed to write PGO profile to file %s", f)
	}
	sp.Info("Stored PGO profile", "path", f, "profile", profile)
	return nil
}

// storeInjectedHash records a hash of the packages that rules add to the build.
// go computes the action ID of a compile from the package as written, so a
// package that only a rule's imports or add_file source bring in never reaches
// it. markedToolVersion folds this hash into the tool ID, so a change in one of
// those packages still invalidates the cached compiles. It always writes the
// file, so a hash recorded by an earlier setup never applies to a later build.
func (sp *setupPhase) storeInjectedHash(ctx context.Context, matched []*rule.InstRuleSet) error {
	hash, err := sp.injectedHash(ctx, matched)
	if err != nil {
		// Like matchInjectedDeps, the build still runs, but a warm build can
		// then reuse a compile made against an older injected package.
		_, _ = fmt.Fprintf(os.Stderr,
			"Warning: could not hash the packages otelc injects into the build; "+
				"a warm build may not pick up changes to them: %v\n", err)
		sp.Warn("could not hash the packages otelc injects into the build", "error", err)
		hash = ""
	}
	f := util.GetInjectedHashFile()
	if err = util.WriteFileAtomic(f, []byte(hash)); err != nil {
		return ex.Wrapf(err, "failed to write injected package hash to file %s", f)
	}
	sp.Info("Stored injected package hash", "path", f, "hash", hash)
	return nil
}

// injectedHash returns a hash of the imports the matched rules declare, the
// source files of their add_file rules, and the dependencies of both.
func (sp *setupPhase) injectedHash(ctx context.Context, matched []*rule.InstRuleSet) (string, error) {
	h := sha256.New()
	var paths []string
	for _, set := range matched {
		paths = appendRuleImports(paths, set.RawRules)
		paths = appendRuleImports(paths, set.FuncRules)
		paths = appendRuleImports(paths, set.StructRules)
		paths = appendRuleImports(paths, set.CallRules)
		paths = appendRuleImports(paths, set.LitRules)
		paths = appendRuleImports(paths, set.DirectiveRules)
		paths = appendRuleImports(paths, set.DeclRules)
		for _, r := range set.FileRules {
			paths = slices.AppendSeq(paths, maps.Values(r.Imports))
			srcImports, err := sp.fileRuleSourceImports(r)
			if err != nil {
				return "", err
			}
			paths = append(paths, srcImports...)
			// An add_file source is often excluded from its own package with
			// //go:build ignore, so the package hash below does not cover it.
			if r.ResolvedPath != "" && r.File != "" {
				if err = hashFile(h, filepath.Join(r.ResolvedPath, r.File)); err != nil {
					return "", err
				}
			}
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	if len(paths) > 0 {
		pkgs, err := pkgload.LoadPackages(ctx,
			packages.NeedName|packages.NeedFiles|packages.NeedEmbedFiles|
				packages.NeedImports|packages.NeedDeps|packages.NeedModule,
			sp.buildFlags, false, paths...)
		if err != nil {
			return "", err
		}
		goroot, err := pkgload.GOROOT(ctx, sp.buildFlags)
		if err != nil {
			return "", err
		}
		if err = hashPackages(h, goroot, pkgs); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)[:8]), nil
}

func appendRuleImports[T interface{ Base() *rule.InstBaseRule }](
	paths []string, rulesByFile map[string][]T,
) []string {
	for _, rules := range rulesByFile {
		for _, r := range rules {
			paths = slices.AppendSeq(paths, maps.Values(r.Base().Imports))
		}
	}
	return paths
}

// hashPackages writes pkgs and their dependencies to w in import path order.
// A package from a versioned module in the module cache is written as its
// module version, since the module cache never changes under a version. Any
// other package, from the main module, a local replace, GOPATH or a vendor
// directory (which has a version from vendor/modules.txt but no module Dir),
// is written as the content of its files. A standard library package, one with no module
// whose files all lie under goroot/src, is skipped: the toolchain version in
// the tool ID covers it.
func hashPackages(w io.Writer, goroot string, pkgs []*packages.Package) error {
	var all []*packages.Package
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		if !isStdPackage(pkg, goroot) {
			all = append(all, pkg)
		}
	})
	slices.SortFunc(all, func(a, b *packages.Package) int { return strings.Compare(a.PkgPath, b.PkgPath) })
	for _, pkg := range all {
		if mod := pkg.Module; mod != nil {
			if mod.Replace != nil {
				mod = mod.Replace
			}
			if mod.Version != "" && mod.Dir != "" {
				_, _ = fmt.Fprintf(w, "%s %s@%s\n", pkg.PkgPath, mod.Path, mod.Version)
				continue
			}
		}
		_, _ = fmt.Fprintf(w, "%s\n", pkg.PkgPath)
		for _, file := range slices.Sorted(slices.Values(packageFiles(pkg))) {
			if err := hashFile(w, file); err != nil {
				return err
			}
		}
	}
	return nil
}

func packageFiles(pkg *packages.Package) []string {
	return slices.Concat(pkg.GoFiles, pkg.OtherFiles, pkg.EmbedFiles)
}

// isStdPackage reports whether pkg belongs to the standard library. go/packages
// does not expose go list's Standard field, so a package counts as standard
// when it has no module and every file lies under goroot/src. An empty goroot
// matches nothing, which hashes more packages rather than fewer.
func isStdPackage(pkg *packages.Package, goroot string) bool {
	if pkg.Module != nil || goroot == "" {
		return false
	}
	src := filepath.Join(goroot, "src") + string(filepath.Separator)
	for _, file := range packageFiles(pkg) {
		if !strings.HasPrefix(filepath.Clean(file), src) {
			return false
		}
	}
	return true
}

// hashFile writes the path and the content hash of a file to w.
func hashFile(w io.Writer, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return ex.Wrapf(err, "reading %s", path)
	}
	sum := sha256.Sum256(content)
	_, _ = fmt.Fprintf(w, "%s %x\n", path, sum)
	return nil
}

// storeResolvedNames persists the import name table built during
// setup, so later toolexec processes need not guess a package name
// from its import path.
func (sp *setupPhase) storeResolvedNames() error {
	bs, err := json.Marshal(sp.resolvedNames)
	if err != nil {
		return ex.Wrapf(err, "failed to marshal import names to JSON")
	}

	f := util.GetImportNamesFile()
	if writeErr := util.WriteFileAtomic(f, bs); writeErr != nil {
		return ex.Wrapf(writeErr, "failed to write import names to file %s", f)
	}
	sp.Info("Stored resolved import names", "path", f, "count", len(sp.resolvedNames))
	return nil
}
