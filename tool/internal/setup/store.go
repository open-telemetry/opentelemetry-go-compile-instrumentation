// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"crypto/rand"
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
	"go.opentelemetry.io/otelc/tool/internal/compileindex"
	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
	"golang.org/x/tools/go/packages"
)

// resolveRulePaths resolves the import paths referenced by function and file rules
// to absolute filesystem paths.
func resolveRulePaths(
	ctx context.Context,
	matched []*rule.InstRuleSet,
	moduleDirs []string,
	buildFlags ...string,
) error {
	dirs := normalizeModuleDirs(moduleDirs)
	pending := collectPendingRulePaths(matched)
	forwarded, flagErr := forwardRulePathFlags(buildFlags)
	if flagErr != nil {
		return flagErr
	}

	resolved := make(map[string]string, len(pending))
	var lastErr error

	for _, moduleDir := range dirs {
		if len(pending) == 0 {
			break
		}

		pkgs, loadErr := packages.Load(&packages.Config{
			Mode:       packages.NeedName | packages.NeedFiles,
			Context:    ctx,
			Dir:        moduleDir,
			BuildFlags: forwarded,
		}, pending...)
		if loadErr != nil {
			lastErr = loadErr
			continue
		}

		next := pending[:0]
		for _, p := range pending {
			pkg, err := findMatchingPackage(pkgs, p)
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

	assignResolvedPaths(matched, resolved)
	return nil
}

func collectPendingRulePaths(matched []*rule.InstRuleSet) []string {
	var pending []string
	for _, ruleset := range matched {
		for _, fileRule := range ruleset.FileRules {
			if fileRule.Path != util.InternalPath {
				pending = append(pending, fileRule.Path)
			}
		}
		for _, funcRule := range ruleset.AllFuncRules() {
			if funcRule.Path != util.InternalPath {
				pending = append(pending, funcRule.Path)
			}
		}
	}
	slices.Sort(pending)
	return slices.Compact(pending)
}

func assignResolvedPaths(matched []*rule.InstRuleSet, resolved map[string]string) {
	for _, ruleset := range matched {
		for _, fileRule := range ruleset.FileRules {
			if fileRule.Path != util.InternalPath {
				fileRule.ResolvedPath = resolved[fileRule.Path]
			}
		}
		for _, funcRule := range ruleset.AllFuncRules() {
			if funcRule.Path != util.InternalPath {
				funcRule.ResolvedPath = resolved[funcRule.Path]
			}
		}
	}
}

// findMatchingPackage returns the single package in pkgs matching importPath, nil if
// none matched (the caller retries it against the next module dir), or an
// error if importPath ambiguously matched more than one package.
func findMatchingPackage(pkgs []*packages.Package, importPath string) (*packages.Package, error) {
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

// store stores the matched rules to the file.
// It's the pair of the InstrumentPhase.load.
// Rule paths must already be resolved via resolveRulePaths.
func (sp *setupPhase) store(matched []*rule.InstRuleSet) error {
	bs, err := json.Marshal(matched)
	if err != nil {
		return ex.Wrapf(err, "failed to marshal rules to JSON")
	}

	if err = writeSetupGeneration(); err != nil {
		return err
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
	if err = sp.storePGOProfile(); err != nil {
		return err
	}
	if err = compileindex.Store(); err != nil {
		sp.Warn("Could not store optional compile index", "error", err)
	}
	return nil
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
// those packages still invalidates the cached compiles. Setup fails when the
// hash cannot be computed: an incomplete hash would let a warm build reuse a
// compile made against an older injected package. A rule import that does not
// load is not part of any compile, so it is left out (see injectedHash).
//
// The hash reads the files on disk, so it misses two inputs of the compile: a
// file replaced through -overlay, where only the overlay's backing file
// changed, and cgo settings such as CGO_CFLAGS that change what an injected
// cgo package exports. Neither is common for the packages rules inject.
func (sp *setupPhase) storeInjectedHash(ctx context.Context, matched []*rule.InstRuleSet) error {
	hash, err := sp.injectedHash(ctx, matched)
	if err != nil {
		return ex.Wrapf(err, "hashing the packages otelc injects into the build")
	}
	f := util.GetInjectedHashFile()
	if err = util.WriteFileAtomic(f, []byte(hash)); err != nil {
		return ex.Wrapf(err, "failed to write injected package hash to file %s", f)
	}
	sp.Info("Stored injected package hash", "path", f, "hash", hash)
	return nil
}

// injectedHash returns a hash of the imports the matched rules declare, the
// source files of their add_file rules, the hook packages of their func rules,
// and the dependencies of all of them.
func (sp *setupPhase) injectedHash(ctx context.Context, matched []*rule.InstRuleSet) (string, error) {
	h := sha256.New()
	paths, err := sp.collectInjectedPaths(h, matched)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return hex.EncodeToString(h.Sum(nil)[:8]), nil
	}
	pkgs, unloadable, err := sp.loadInjectedPackages(ctx, paths)
	if err != nil {
		return "", err
	}
	// A rule import that does not load from the main module never reaches
	// a cached compile: a compile that adds it resolves it with the same
	// go list and fails, and the link skips it. Its path still goes into
	// the hash, so the hash changes once it loads.
	if len(unloadable) > 0 {
		slices.Sort(unloadable)
		sp.Info("Leaving rule imports that do not load out of the injected package hash",
			"imports", unloadable)
		for _, path := range unloadable {
			_, _ = fmt.Fprintf(h, "%s unloadable\n", path)
		}
	}
	goroot, err := pkgload.GOROOT(ctx, sp.buildFlags)
	if err != nil {
		return "", err
	}
	if err = hashPackages(h, goroot, pkgs); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)[:8]), nil
}

func (sp *setupPhase) collectInjectedPaths(h io.Writer, matched []*rule.InstRuleSet) ([]string, error) {
	var paths []string
	for _, set := range matched {
		paths = appendRuleImports(paths, set.RawRules)
		paths = appendRuleImports(paths, set.FuncRules)
		paths = appendRuleImports(paths, set.StructRules)
		paths = appendRuleImports(paths, set.CallRules)
		paths = appendRuleImports(paths, set.LitRules)
		paths = appendRuleImports(paths, set.DirectiveRules)
		paths = appendRuleImports(paths, set.DeclRules)
		for _, r := range set.AllFuncRules() {
			if r.Path != "" && r.Path != util.InternalPath {
				paths = append(paths, r.Path)
			}
		}
		for _, r := range set.FileRules {
			paths = slices.AppendSeq(paths, maps.Values(r.Imports))
			srcImports, err := sp.fileRuleSourceImports(r)
			if err != nil {
				return nil, err
			}
			paths = append(paths, srcImports...)
			// An add_file source is often excluded from its own package with
			// //go:build ignore, so the package hash below does not cover it.
			if r.ResolvedPath != "" && r.File != "" {
				if err = hashFileAs(h, filepath.Join(r.ResolvedPath, r.File)); err != nil {
					return nil, err
				}
			}
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

// loadInjectedPackages loads the rule imports in roots and returns the ones
// that do not load, such as a package of a module that is in the module graph
// but not in go.mod. go list then fails the whole load ("updates to go.mod
// needed"), so a failed load is split in two halves until each such root is
// found. Any other error is returned.
func (sp *setupPhase) loadInjectedPackages(
	ctx context.Context,
	roots []string,
) ([]*packages.Package, []string, error) {
	pkgs, err := pkgload.LoadPackages(ctx,
		packages.NeedName|packages.NeedFiles|packages.NeedEmbedFiles|
			packages.NeedImports|packages.NeedDeps|packages.NeedModule,
		sp.buildFlags, false, roots...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}
	if err != nil {
		if !pkgload.IsLoadFailure(err) {
			return nil, nil, err
		}
		if len(roots) == 1 {
			return nil, roots, nil
		}
		const halves = 2
		mid := len(roots) / halves
		var loaded []*packages.Package
		var unloadable []string
		for _, half := range [][]string{roots[:mid], roots[mid:]} {
			one, oneUnloadable, halfErr := sp.loadInjectedPackages(ctx, half)
			if halfErr != nil {
				return nil, nil, halfErr
			}
			loaded = append(loaded, one...)
			unloadable = append(unloadable, oneUnloadable...)
		}
		return loaded, unloadable, nil
	}

	// A root that does not exist, or that imports a package that does not,
	// comes back with the errors on the missing package instead of failing the
	// load.
	loaded := make([]*packages.Package, 0, len(pkgs))
	var unloadable []string
	for _, pkg := range pkgs {
		if onlyLoadFailures(pkg) {
			unloadable = append(unloadable, pkg.PkgPath)
			continue
		}
		loaded = append(loaded, pkg)
	}
	return loaded, unloadable, nil
}

// onlyLoadFailures reports whether pkg or a package it imports has errors, and
// all of them are load failures. A root with any other error stays loaded, so
// hashPackages fails on it.
func onlyLoadFailures(pkg *packages.Package) bool {
	var failed, other bool
	packages.Visit([]*packages.Package{pkg}, nil, func(p *packages.Package) {
		for _, err := range p.Errors {
			if isLoadFailure(err) {
				failed = true
			} else {
				other = true
			}
		}
	})
	return failed && !other
}

// isLoadFailure reports whether err says that go list could not load a
// package. A parse or type error is never one, even when its message quotes a
// load failure.
func isLoadFailure(err packages.Error) bool {
	return (err.Kind == packages.ListError || err.Kind == packages.UnknownError) && pkgload.IsLoadFailure(err)
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
// is written as the go version of its module and the content of its files.
// A standard library package, one with no module
// whose files all lie under goroot/src, is skipped: the toolchain version in
// the tool ID covers it.
func hashPackages(w io.Writer, goroot string, pkgs []*packages.Package) error {
	var all []*packages.Package
	var loadErr error
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		// packages.Load reports a package it could not fully load in
		// pkg.Errors, with files or dependencies missing from the hash.
		if len(pkg.Errors) > 0 && loadErr == nil {
			loadErr = ex.Newf("loading package %q: %v", pkg.PkgPath, pkg.Errors[0])
		}
		if !isStdPackage(pkg, goroot) {
			all = append(all, pkg)
		}
	})
	if loadErr != nil {
		return loadErr
	}
	slices.SortFunc(all, func(a, b *packages.Package) int { return strings.Compare(a.PkgPath, b.PkgPath) })
	for _, pkg := range all {
		var goVersion string
		if mod := pkg.Module; mod != nil {
			if mod.Replace != nil {
				mod = mod.Replace
			}
			if mod.Version != "" && mod.Dir != "" {
				_, _ = fmt.Fprintf(w, "%s %s@%s\n", pkg.PkgPath, mod.Path, mod.Version)
				continue
			}
			goVersion = mod.GoVersion
		}
		// The go directive of the module sets the language version, which can
		// change what the same files compile to (loop variable scope, for one).
		_, _ = fmt.Fprintf(w, "%s go=%s\n", pkg.PkgPath, goVersion)
		for _, file := range slices.Sorted(slices.Values(packageFiles(pkg))) {
			if err := hashFileAs(w, file); err != nil {
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

// hashFileAs writes a file's content hash and a name that is independent of
// its invocation directory. User paths outside that directory stay intact.
func hashFileAs(w io.Writer, path string) error {
	name := string(instrument.WithoutWorkDirPaths([]byte(path)))
	content, err := os.ReadFile(path)
	if err != nil {
		return ex.Wrapf(err, "reading %s", path)
	}
	sum := sha256.Sum256(content)
	_, _ = fmt.Fprintf(w, "%s %x\n", name, sum)
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

// setupGenerationBytes is the size of the random setup generation token.
const setupGenerationBytes = 16

// writeSetupGeneration records a fresh token for this setup. The resolution
// scope written later carries it, so session.Open can tell a scope this setup
// wrote from one an earlier setup left behind, without relying on file times.
func writeSetupGeneration() error {
	token := make([]byte, setupGenerationBytes)
	if _, err := rand.Read(token); err != nil {
		return ex.Wrapf(err, "generating the setup generation token")
	}
	f := util.GetSetupGenerationFile()
	if err := util.WriteFileAtomic(f, []byte(hex.EncodeToString(token))); err != nil {
		return ex.Wrapf(err, "failed to write setup generation to file %s", f)
	}
	return nil
}
