// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	toolruntime "go.opentelemetry.io/otelc/tool/internal/runtime"
	"go.opentelemetry.io/otelc/tool/util"
	"golang.org/x/tools/go/packages"
)

// resolveRulePaths resolves the import paths referenced by function and file rules
// to absolute filesystem paths.
func resolveRulePaths(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs []string) error {
	dirs := normalizeModuleDirs(moduleDirs)
	pending := collectPendingRulePaths(matched)

	resolved := make(map[string]string, len(pending))
	var lastErr error

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
	for _, stale := range []string{util.GetRulesDigestFile(), util.GetInjectedHashFile()} {
		if err := os.Remove(stale); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return ex.Wrapf(err, "removing stale %s", stale)
		}
	}

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

	namesBytes, err := sp.storeResolvedNames()
	if err != nil {
		return err
	}
	if err = sp.storePGOProfile(); err != nil {
		return err
	}
	if err = compileindex.StoreFromArtifacts(bs, namesBytes, matched); err != nil {
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

func (sp *setupPhase) hashInjectedPaths(ctx context.Context, h io.Writer, paths []string) error {
	slices.Sort(paths)
	paths = slices.Compact(paths)
	if len(paths) == 0 {
		return nil
	}
	pkgs, unloadable, err := sp.loadInjectedPackages(ctx, paths)
	if err != nil {
		return err
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
		return err
	}
	return hashPackages(h, goroot, pkgs)
}

// storeRulesDigest computes and atomically writes the stable 16-hex-char digest
// used for the -V=full tool ID. Decoupled from target-specific matched.json so
// different targets in the same module with identical loaded rules share Go's
// build cache.
func (sp *setupPhase) storeRulesDigest(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs []string) error {
	res, err := sp.computeRulesDigest(ctx, matched, moduleDirs, toolruntime.RuntimeGLSSource)
	if err != nil {
		return ex.Wrapf(err, "computing rules digest")
	}
	injFile := util.GetInjectedHashFile()
	if err = util.WriteFileAtomic(injFile, []byte(res.injected)); err != nil {
		return ex.Wrapf(err, "failed to write injected package hash to file %s", injFile)
	}
	f := util.GetRulesDigestFile()
	if err = util.WriteFileAtomic(f, []byte(res.digest)); err != nil {
		return ex.Wrapf(err, "failed to write rules digest to file %s", f)
	}
	sp.Info("Stored rules digest", "path", f, "digest", res.digest)
	return nil
}

type rulesDigestResult struct {
	digest   string
	injected string
}

// computeRulesDigest computes the 16-hex-char tool-ID digest and the 16-hex-char
// injected-package hash from all loaded rules (not target-specific matched),
// active build tags, embedded GLS source, and the global ignore-directive bit.
func (sp *setupPhase) computeRulesDigest(
	ctx context.Context,
	matched []*rule.InstRuleSet,
	moduleDirs []string,
	glsSource []byte,
) (rulesDigestResult, error) {
	h := sha256.New()

	usesRoot, err := hashCanonicalRules(h, sp.loadedRules)
	if err != nil {
		return rulesDigestResult{}, err
	}
	if usesRoot {
		roots := slices.Clone(sp.rootModulePaths)
		slices.Sort(roots)
		_, _ = fmt.Fprintf(h, "roots=%s\n", strings.Join(roots, ","))
	}

	injected, err := sp.injectedHashForLoadedRules(ctx, moduleDirs)
	if err != nil {
		return rulesDigestResult{}, err
	}
	_, _ = fmt.Fprintf(h, "injected=%s\n", injected)

	if tags := sp.activeBuildTags(); len(tags) > 0 {
		sortedTags := slices.Clone(tags)
		slices.Sort(sortedTags)
		sortedTags = slices.Compact(sortedTags)
		_, _ = fmt.Fprintf(h, "tags=%s\n", strings.Join(sortedTags, ","))
	}

	glsSum := sha256.Sum256(glsSource)
	_, _ = fmt.Fprintf(h, "gls=%x\n", glsSum)

	ignoreBit := 0
	if rule.AnySetHasIgnoredCallFiles(matched) {
		ignoreBit = 1
	}
	_, _ = fmt.Fprintf(h, "ignore=%d\n", ignoreBit)

	return rulesDigestResult{
		digest:   hex.EncodeToString(h.Sum(nil)[:8]),
		injected: injected,
	}, nil
}

func hashCanonicalRules(w io.Writer, rules []rule.InstRule) (bool, error) {
	encoded := make([]string, 0, len(rules))
	usesRoot := false
	for _, r := range rules {
		if r.GetTarget().UsesRoot() {
			usesRoot = true
		}
		entry, err := canonicalRuleJSON(r)
		if err != nil {
			return false, err
		}
		encoded = append(encoded, entry)
	}
	slices.Sort(encoded)
	for _, entry := range encoded {
		_, _ = fmt.Fprintf(w, "rule=%s\n", entry)
	}
	return usesRoot, nil
}

func (sp *setupPhase) injectedHashForLoadedRules(ctx context.Context, moduleDirs []string) (string, error) {
	resolvedDirs := resolveFileRuleDirs(ctx, sp.loadedRules, moduleDirs, sp.buildFlags)
	type addFileEntry struct {
		normalized string
		path       string
	}
	var importPaths []string
	var addFiles []addFileEntry
	for _, r := range sp.loadedRules {
		if br, ok := r.(interface{ Base() *rule.InstBaseRule }); ok {
			importPaths = slices.AppendSeq(importPaths, maps.Values(br.Base().Imports))
		}
		switch rt := r.(type) {
		case *rule.InstFuncRule:
			if rt.Path != "" && rt.Path != util.InternalPath {
				importPaths = append(importPaths, rt.Path)
			}
		case *rule.InstFileRule:
			dir := resolvedFileRuleDir(rt, resolvedDirs)
			if dir == "" || rt.File == "" {
				continue
			}
			fullPath := filepath.Join(dir, rt.File)
			addFiles = append(addFiles, addFileEntry{
				normalized: string(instrument.WithoutWorkDirPaths([]byte(fullPath))),
				path:       fullPath,
			})
			cp := *rt
			cp.ResolvedPath = dir
			srcImports, err := sp.fileRuleSourceImports(&cp)
			if err != nil {
				return "", err
			}
			importPaths = append(importPaths, srcImports...)
		}
	}
	slices.SortFunc(addFiles, func(a, b addFileEntry) int {
		return strings.Compare(a.normalized, b.normalized)
	})
	addFiles = slices.CompactFunc(addFiles, func(a, b addFileEntry) bool {
		return a.normalized == b.normalized
	})

	injHash := sha256.New()
	for _, af := range addFiles {
		if err := hashFileAs(injHash, af.path); err != nil {
			return "", err
		}
	}
	if err := sp.hashInjectedPaths(ctx, injHash, importPaths); err != nil {
		return "", err
	}
	return hex.EncodeToString(injHash.Sum(nil)[:8]), nil
}

func resolvedFileRuleDir(fr *rule.InstFileRule, resolvedDirs map[string]string) string {
	if fr.ResolvedPath != "" {
		return fr.ResolvedPath
	}
	if fr.Path != "" && fr.Path != util.InternalPath {
		return resolvedDirs[fr.Path]
	}
	return ""
}

func (sp *setupPhase) activeBuildTags() []string {
	buildFlags := sp.buildFlags
	if len(buildFlags) == 0 {
		buildFlags = util.GetBuildFlags()
	}
	return util.UserBuildTags(util.SplitGoflags(os.Getenv("GOFLAGS")), buildFlags)
}

func canonicalRuleJSON(r rule.InstRule) (string, error) {
	val := any(r)
	switch rt := r.(type) {
	case *rule.InstFuncRule:
		cp := *rt
		cp.ResolvedPath = ""
		val = &cp
	case *rule.InstFileRule:
		cp := *rt
		cp.ResolvedPath = ""
		val = &cp
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return "", ex.Wrapf(err, "marshaling rule %q for digest", r.GetName())
	}
	return fmt.Sprintf("%T:%s", r, instrument.WithoutWorkDirPaths(raw)), nil
}

// resolveFileRuleDirs best-effort resolves the package directories of loaded
// file rules that were not already resolved during matching.
func resolveFileRuleDirs(
	ctx context.Context,
	rules []rule.InstRule,
	moduleDirs, buildFlags []string,
) map[string]string {
	var pending []string
	for _, r := range rules {
		fr, ok := r.(*rule.InstFileRule)
		if !ok || fr.ResolvedPath != "" || fr.Path == "" || fr.Path == util.InternalPath {
			continue
		}
		pending = append(pending, fr.Path)
	}
	if len(pending) == 0 {
		return nil
	}
	slices.Sort(pending)
	pending = slices.Compact(pending)

	dirs := normalizeModuleDirs(moduleDirs)
	resolved := make(map[string]string, len(pending))
	for _, moduleDir := range dirs {
		if len(pending) == 0 {
			break
		}
		found := resolveDirsInModule(ctx, moduleDir, buildFlags, pending)
		next := pending[:0]
		for _, p := range pending {
			if dir, ok := found[p]; ok {
				resolved[p] = dir
			} else {
				next = append(next, p)
			}
		}
		pending = next
	}
	return resolved
}

func resolveDirsInModule(ctx context.Context, moduleDir string, buildFlags, paths []string) map[string]string {
	pkgs, err := packages.Load(&packages.Config{
		Mode:       packages.NeedName | packages.NeedFiles,
		Context:    ctx,
		Dir:        moduleDir,
		BuildFlags: buildFlags,
	}, paths...)
	if err != nil {
		if len(paths) <= 1 || !pkgload.IsLoadFailure(err) {
			return nil
		}
		const halves = 2
		mid := len(paths) / halves
		left := resolveDirsInModule(ctx, moduleDir, buildFlags, paths[:mid])
		right := resolveDirsInModule(ctx, moduleDir, buildFlags, paths[mid:])
		if left == nil {
			return right
		}
		maps.Copy(left, right)
		return left
	}
	resolved := make(map[string]string, len(pkgs))
	for _, p := range paths {
		pkg, findErr := findMatchingPackage(pkgs, p)
		if findErr == nil && pkg != nil {
			resolved[p] = pkg.Dir
		}
	}
	return resolved
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
func (sp *setupPhase) storeResolvedNames() ([]byte, error) {
	bs, err := json.Marshal(sp.resolvedNames)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to marshal import names to JSON")
	}

	f := util.GetImportNamesFile()
	if writeErr := util.WriteFileAtomic(f, bs); writeErr != nil {
		return nil, ex.Wrapf(writeErr, "failed to write import names to file %s", f)
	}
	sp.Info("Stored resolved import names", "path", f, "count", len(sp.resolvedNames))
	return bs, nil
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
