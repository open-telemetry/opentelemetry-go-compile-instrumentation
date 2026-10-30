// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	goast "go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	goversion "go/version"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"go.opentelemetry.io/otelc/tool/data"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// A vendored project keeps vendor mode: its dependencies come only from
// vendor/. The hook code of otelc needs more modules (the OpenTelemetry SDK,
// the otelc modules, ...). vendorSync adds them to vendor/ like this:
//
//  1. It makes a scratch module in .otelc-build/vendor-scratch. The scratch
//     module imports the packages of the hook code. It requires each module
//     that the user vendors, at the vendored version. Each of these modules
//     is replaced by a copy of its vendored packages (with a generated
//     go.mod), so the go command does not download it. On Linux, the copy
//     uses hard links when possible. A module that go.mod replaces with a local
//     directory is replaced by this directory. The go.sum file of the user
//     is copied too, so that the go command checks a module that it gets
//     against the checksums of the user.
//  2. It runs "go mod tidy" and "go mod vendor" in the scratch module. Only
//     the modules that the user does not vendor are downloaded (or taken from
//     the module cache). There is one exception: when the hook code needs a
//     package of a vendored module that vendor/ does not have, the go
//     command gets this module at the vendored version. The files that are
//     in vendor/ already are never replaced.
//  3. It merges the vendor/modules.txt of the scratch module into the one of
//     the user (see mergeVendorModules), copies the new packages into the
//     vendor/ of the user, and adds the require and replace directives to
//     go.mod, so that the vendor consistency check of the go command passes.
//
// When the hook code needs a higher version of a vendored module, a build
// fails with a *vendorConflictError. `otelc vendor` (see Vendor) gets the
// higher version instead, like "go get" and "go mod vendor" do: it replaces
// the vendored files of this module and changes go.mod.
//
// When a state manager is in the context, all the changes are tracked. For
// otelc go build and otelc setup, the cleanup removes them after the build.
// otelc vendor reverts them when it fails, and discards the saved state when
// it succeeds, so the changes stay. Without a state manager (otelc pin) the
// changes stay.

const (
	vendorScratchDir     = "vendor-scratch"
	vendorScratchDepsDir = "deps"
	vendorScratchModule  = "otelc.invalid/vendorscratch"
	vendorScratchFile    = "imports.go"
	modulesTxtFileName   = "modules.txt"
	goSumFileName        = "go.sum"
	vendorFilePerm       = 0o644
)

// vendorUpdateKey is the context key that permits vendorSync to replace
// vendored modules with higher versions.
type vendorUpdateKey struct{}

// contextWithVendorUpdate returns a copy of ctx that permits vendorSync to
// replace a vendored module with the higher version that the hook code needs.
// Only `otelc vendor` does this: a build never changes vendored code.
func contextWithVendorUpdate(ctx context.Context) context.Context {
	return context.WithValue(ctx, vendorUpdateKey{}, true)
}

func vendorUpdateFromContext(ctx context.Context) bool {
	update, _ := ctx.Value(vendorUpdateKey{}).(bool)
	return update
}

// bundleFingerprint returns a short hash of the otelc version and of the
// embedded otelc bundle. otelc writes it in an annotation of the otelc
// modules that it adds to vendor/modules.txt. Thus a different otelc (with
// the same import paths, but different code) finds that vendor/ is old.
//
//nolint:gochecknoglobals // computed one time
var bundleFingerprint = sync.OnceValue(func() string {
	h := sha256.New()
	_, _ = io.WriteString(h, util.Version+"\n")
	_, _ = io.Copy(h, data.GetBundleReader())
	const fingerprintLen = 16
	return hex.EncodeToString(h.Sum(nil))[:fingerprintLen]
})

// vendorSync makes the vendor directory of the module in moduleDir hold all
// the modules that the hook code needs. imports are the imports of
// otel.instrumentation.go.
func vendorSync(ctx context.Context, imports map[string]bool, moduleDir string) error {
	if len(imports) == 0 {
		return nil
	}
	logger := util.LoggerFromContext(ctx)

	if err := extractOtelcBundle(); err != nil {
		return ex.Wrapf(err, "extracting the otelc bundle")
	}

	vendorDir := filepath.Join(moduleDir, vendorDirName)
	modulesTxtPath := filepath.Join(vendorDir, modulesTxtFileName)
	if err := checkNoSymlink(moduleDir, modulesTxtPath); err != nil {
		return err
	}
	user, err := readModulesTxt(modulesTxtPath)
	if err != nil {
		return err
	}
	userMod, err := parseGoMod(filepath.Join(moduleDir, goModFileName))
	if err != nil {
		return err
	}
	sources := &hookSources{
		moduleDir: moduleDir,
		user:      user,
		localDirs: localModuleDirs(user, moduleDir),
	}
	if userMod.Module != nil {
		sources.mainPath = userMod.Module.Mod.Path
	}

	scratchImports, err := vendorScratchImports(imports, sources)
	if err != nil {
		return err
	}
	needed := scratchImports.packages
	replaces, err := otelcReplaces(scratchImports.toolImports)
	if err != nil {
		return err
	}
	fingerprint := bundleFingerprint()
	stale := staleOtelcModules(user, needed, replaces, fingerprint)
	missing := slices.DeleteFunc(slices.Clone(needed), user.hasPackage)
	if len(missing) == 0 && len(stale) == 0 {
		logger.InfoContext(ctx, "vendor directory already has the packages of the hook code",
			"vendor", vendorDir)
		return nil
	}
	if len(stale) > 0 {
		logger.InfoContext(ctx, "vendor directory has otelc modules of a different otelc version; "+
			"they are replaced", "vendor", vendorDir, "modules", slices.Sorted(maps.Keys(stale)))
	}
	logger.InfoContext(ctx, "adding the modules of the hook code to the vendor directory",
		"vendor", vendorDir, "missing", missing)

	return syncVendorModules(ctx, vendorSyncInput{
		moduleDir:   moduleDir,
		userMod:     userMod,
		needed:      needed,
		user:        user,
		stale:       stale,
		replaces:    replaces,
		localDirs:   sources.localDirs,
		fingerprint: fingerprint,
	})
}

// vendorSyncInput is the input of syncVendorModules.
type vendorSyncInput struct {
	moduleDir string
	// userMod is the parsed go.mod file of the user. syncVendorModules
	// changes it.
	userMod *modfile.File
	// needed are the packages that the scratch module imports.
	needed []string
	// user is the vendor/modules.txt of the user.
	user *modulesTxt
	// stale are the otelc modules of user that are replaced.
	stale map[string]bool
	// replaces are the otelc modules and their directories in the bundle.
	replaces map[string]string
	// localDirs are the vendored modules that go.mod replaces with a local
	// directory, and their absolute directories (see localModuleDirs).
	localDirs map[string]string
	// fingerprint is the fingerprint of the otelc bundle.
	fingerprint string
}

// syncVendorModules resolves the modules of the hook code in the scratch
// module, and merges them into the module of the user.
func syncVendorModules(ctx context.Context, in vendorSyncInput) error {
	userMod := in.userMod
	goVersion := ""
	if userMod.Go != nil {
		goVersion = userMod.Go.Version
	}

	scratch, err := buildVendorScratch(ctx, vendorScratchInput{
		user:          in.user.without(in.stale),
		original:      in.user,
		replacedMods:  in.stale,
		userVendorDir: filepath.Join(in.moduleDir, vendorDirName),
		imports:       in.needed,
		replaces:      in.replaces,
		localDirs:     in.localDirs,
		goVersion:     goVersion,
		update:        vendorUpdateFromContext(ctx),
	})
	if err != nil {
		return err
	}

	changed, err := mergeScratchModules(ctx, in, scratch, userMod)
	if err != nil || !changed {
		return err
	}

	reportVendorBumps(ctx, in.user, scratch)
	if userMod.Go != nil && userMod.Go.Version != goVersion {
		_, _ = fmt.Fprintf(os.Stdout, "Bumped go version (%s -> %s)\n", goVersion, userMod.Go.Version)
		util.LoggerFromContext(ctx).WarnContext(ctx, "bumped go version", "old", goVersion, "new", userMod.Go.Version)
	}
	return nil
}

// mergeScratchModules merges the vendor directory of the scratch module into
// the module of the user, and changes userMod. It reports whether the module
// of the user changed.
func mergeScratchModules(ctx context.Context, in vendorSyncInput, scratch *vendorScratch, userMod *modfile.File) (
	bool, error,
) {
	injected, err := readModulesTxt(filepath.Join(scratch.dir, vendorDirName, modulesTxtFileName))
	if err != nil {
		return false, err
	}
	// The scratch module takes the vendored modules of the user at their
	// vendored version. Their version in the scratch module can be higher
	// (minimal version selection), but findVendorConflicts found that no
	// package of the hook code needs it. The updated modules keep the version
	// of the scratch module.
	injected = atVendoredVersions(injected, in.user, scratch.fixedMods())

	// The stale and the updated modules are removed from vendor/, and then
	// added again from the scratch module.
	removed := maps.Clone(in.stale)
	for path := range scratch.updated {
		removed[path] = true
	}
	base := in.user.without(removed)
	merged, added, err := mergeVendorModules(base, injected)
	if err != nil {
		return false, err
	}
	if len(added) == 0 && len(removed) == 0 {
		return false, nil
	}
	setBundleFingerprint(merged, base, in.fingerprint)

	dropStaleReplaces(userMod, in.user, in.stale)
	if err = updateGoModForVendor(userMod, base, merged, in.moduleDir); err != nil {
		return false, err
	}

	deleted, err := vendoredModuleFiles(in.user, removed, in.moduleDir)
	if err != nil {
		return false, err
	}
	// A build does not need go.sum (vendor mode does not read it), and it
	// must not change it. `otelc vendor` adds the checksums, like "go get".
	var goSum []byte
	if vendorUpdateFromContext(ctx) {
		if goSum, err = mergedGoSum(in.moduleDir, scratch.dir); err != nil {
			return false, err
		}
	}
	err = applyVendorMerge(ctx, in.moduleDir, filepath.Join(scratch.dir, vendorDirName), vendorMergeResult{
		goMod:   userMod,
		goSum:   goSum,
		merged:  merged,
		added:   added,
		deleted: deleted,
	})
	if err != nil {
		return false, err
	}
	// The new versions are in merged: keep them for reportVendorBumps.
	for path := range scratch.updated {
		scratch.versions[path], _ = merged.Module(path)
	}
	return true, nil
}

// reportVendorBumps prints the vendored modules that vendorSync changed to a
// higher version.
func reportVendorBumps(ctx context.Context, user *modulesTxt, scratch *vendorScratch) {
	logger := util.LoggerFromContext(ctx)
	for _, path := range slices.Sorted(maps.Keys(scratch.updated)) {
		oldVersion, _ := user.Module(path)
		newVersion := scratch.versions[path]
		_, _ = fmt.Fprintf(os.Stdout, "Bumped dependency %s (%s -> %s)\n", path, oldVersion, newVersion)
		logger.WarnContext(ctx, "bumped a vendored dependency for the hook code",
			"module", path, "old", oldVersion, "new", newVersion)
	}
}

// staleOtelcModules returns the otelc modules in user that do not come from
// this otelc bundle, and that must be replaced by the modules of the bundle:
//   - the otelc modules that otelc added (they have a bundle annotation) with
//     a different fingerprint.
//   - the modules of the bundle (replaces, for example pkg/runtime) without
//     the fingerprint of this bundle. The hook code can use them through its
//     own imports, so they must have the code of the bundle, as in a build
//     without vendor/.
//   - the otelc modules without the fingerprint of this bundle that have
//     packages that the hook code needs.
//
// These modules come from a different otelc version (or from the user), so
// their code can be different from the code of the bundle. The other otelc
// modules of the user (for example the otelc tool) stay. A module that is not
// in the bundle and that go.mod replaces with a local directory is the code
// of the user (for example the local instrumentation of a test app below
// go.opentelemetry.io/otelc/test), so it stays too.
func staleOtelcModules(
	user *modulesTxt,
	needed []string,
	replaces map[string]string,
	fingerprint string,
) map[string]bool {
	stale := make(map[string]bool)
	for _, mod := range user.usedModules() {
		if !isOtelcModule(mod.Path) {
			continue
		}
		annotation := mod.annotation(annotationBundlePrefix)
		_, inBundle := replaces[mod.Path]
		if (annotation != "" || inBundle) && annotation != fingerprint {
			stale[mod.Path] = true
		}
	}
	for _, pkg := range needed {
		mod := user.moduleOfPackage(pkg)
		if mod == nil || !isOtelcModule(mod.Path) {
			continue
		}
		annotation := mod.annotation(annotationBundlePrefix)
		if _, inBundle := replaces[mod.Path]; annotation == "" && !inBundle && isDirReplacement(mod.Replacement) {
			continue
		}
		if annotation != fingerprint {
			stale[mod.Path] = true
		}
	}
	return stale
}

// setBundleFingerprint adds the bundle annotation to the otelc modules that
// merged has and base does not have.
func setBundleFingerprint(merged, base *modulesTxt, fingerprint string) {
	for _, mod := range merged.usedModules() {
		if isOtelcModule(mod.Path) && base.find(mod.Path) == nil {
			mod.setAnnotation(annotationBundlePrefix, fingerprint)
		}
	}
}

// dropStaleReplaces removes from mf the version-specific replace directives
// of the stale otelc modules. updateGoModForVendor adds new ones.
func dropStaleReplaces(mf *modfile.File, user *modulesTxt, stale map[string]bool) {
	for path := range stale {
		version, ok := user.Module(path)
		if !ok {
			continue
		}
		for _, r := range mf.Replace {
			if r.Old.Path == path && r.Old.Version == version {
				_ = mf.DropReplace(path, version)
				break
			}
		}
	}
	mf.Cleanup()
}

// vendoredModuleFiles returns the vendored files of the modules in paths:
// the files of their packages (with the embedded files in subdirectories,
// see packageFiles), and the files of the parent directories (up to the
// module root) that are not packages, such as LICENSE.
func vendoredModuleFiles(user *modulesTxt, paths map[string]bool, moduleDir string) ([]string, error) {
	vendorDir := filepath.Join(moduleDir, vendorDirName)
	own := vendoredDirsOf(user)
	isPackage := vendoredPackages(user)
	seenDirs := make(map[string]bool)
	var files []string
	for _, mod := range user.usedModules() {
		if !paths[mod.Path] {
			continue
		}
		for _, pkg := range mod.Packages {
			if seenDirs[pkg] {
				continue
			}
			seenDirs[pkg] = true
			pkgFiles, err := packageFiles(moduleDir, filepath.Join(vendorDir, filepath.FromSlash(pkg)), pkg, own)
			if err != nil {
				return nil, err
			}
			files = append(files, pkgFiles...)
			for _, parent := range parentDirs(pkg, mod.Path) {
				if isPackage[parent] || seenDirs[parent] {
					continue
				}
				seenDirs[parent] = true
				dirFiles, dirErr := regularFiles(moduleDir, filepath.Join(vendorDir, filepath.FromSlash(parent)))
				if dirErr != nil {
					return nil, dirErr
				}
				files = append(files, dirFiles...)
			}
		}
	}
	// A file of a parent directory can also be an embedded file of a
	// package above it.
	slices.Sort(files)
	return slices.Compact(files), nil
}

// regularFiles returns the regular files in dir, which is below root. A
// directory that does not exist has no files. A symbolic link is an error.
func regularFiles(root, dir string) ([]string, error) {
	if err := checkNoSymlink(root, dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, ex.Wrapf(err, "reading %s", dir)
	}
	var files []string
	for _, e := range entries {
		switch {
		case e.Type()&os.ModeSymlink != 0:
			return nil, ex.Newf("%s is a symbolic link: otelc does not follow symbolic links in vendor directories",
				filepath.Join(dir, e.Name()))
		case e.Type().IsRegular():
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// mergedGoSum returns the content of the go.sum file of the module in
// moduleDir, with the lines of the go.sum of the scratch module that it does
// not have. Thus the go command can check the modules that otelc added or
// updated, for example in a later "go mod vendor". It returns nil when
// nothing changes.
func mergedGoSum(moduleDir, scratchDir string) ([]byte, error) {
	userSum, err := os.ReadFile(filepath.Join(moduleDir, goSumFileName))
	if err != nil && !os.IsNotExist(err) {
		return nil, ex.Wrap(err)
	}
	scratchSum, err := os.ReadFile(filepath.Join(scratchDir, goSumFileName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, ex.Wrap(err)
	}
	out, changed := mergeGoSumLines(userSum, scratchSum)
	if !changed {
		return nil, nil
	}
	return out, nil
}

// mergeGoSumLines returns base with the lines of extra that it does not
// have, and reports whether lines were added.
func mergeGoSumLines(base, extra []byte) ([]byte, bool) {
	have := make(map[string]bool)
	for line := range strings.Lines(string(base)) {
		have[strings.TrimSpace(line)] = true
	}
	out := slices.Clone(base)
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	changed := false
	for line := range strings.Lines(string(extra)) {
		line = strings.TrimSpace(line)
		const goSumFields = 3
		if len(strings.Fields(line)) != goSumFields || have[line] {
			continue
		}
		out = append(out, line+"\n"...)
		have[line] = true
		changed = true
	}
	return out, changed
}

// writeScratchGoSum puts the lines of the go.sum file of the module in
// moduleDir (if it exists) first in the go.sum file of the scratch module.
// Then the go command checks a module that it gets at a vendored version
// (see vendorScratch.pin) against the checksums of the user, also when
// GONOSUMDB or GOPRIVATE disable the checksum database for it. It is
// necessary before each round: "go mod tidy" removes the lines of the modules
// that the scratch module replaces with a directory. mergedGoSum adds the new
// lines to the user.
func writeScratchGoSum(moduleDir, scratchDir string) error {
	userSum, err := os.ReadFile(filepath.Join(moduleDir, goSumFileName))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return ex.Wrapf(err, "reading %s", filepath.Join(moduleDir, goSumFileName))
	}
	scratchPath := filepath.Join(scratchDir, goSumFileName)
	scratchSum, err := os.ReadFile(scratchPath)
	if err != nil && !os.IsNotExist(err) {
		return ex.Wrap(err)
	}
	out, _ := mergeGoSumLines(userSum, scratchSum)
	return util.WriteFileAtomic(scratchPath, out, vendorFilePerm)
}

// vendorMergeResult is the new state of the module of the user.
type vendorMergeResult struct {
	// goMod is the changed go.mod file.
	goMod *modfile.File
	// goSum is the new content of go.sum, or nil when it does not change.
	goSum []byte
	// merged is the changed vendor/modules.txt file.
	merged *modulesTxt
	// added are the import paths of the packages to copy to vendor/.
	added []string
	// deleted are the vendored files to remove before the copy.
	deleted []string
}

// readModulesTxt reads and parses the vendor/modules.txt file at path.
func readModulesTxt(path string) (*modulesTxt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ex.Wrapf(err, "reading %s", path)
	}
	m, err := parseModulesTxt(data)
	if err != nil {
		return nil, ex.Wrapf(err, "parsing %s", path)
	}
	return m, nil
}

// applyVendorMerge writes the merge result to the module in moduleDir: it
// removes the deleted files, copies the added packages from scratchVendor to
// vendor/, and writes vendor/modules.txt, go.mod and go.sum. When a state
// manager is in the context, it tracks all the changed paths first, so that
// the cleanup restores them.
func applyVendorMerge(ctx context.Context, moduleDir, scratchVendor string, res vendorMergeResult) error {
	vendorDir := filepath.Join(moduleDir, vendorDirName)
	modulesTxtPath := filepath.Join(vendorDir, modulesTxtFileName)
	goModPath := filepath.Join(moduleDir, goModFileName)
	goSumPath := filepath.Join(moduleDir, goSumFileName)

	deleted := make(map[string]bool, len(res.deleted))
	for _, path := range res.deleted {
		deleted[path] = true
	}
	copies, dirs, err := planVendorCopies(res.merged, res.added, vendorCopyRoots{
		scratchVendor: scratchVendor,
		moduleDir:     moduleDir,
		deleted:       deleted,
	})
	if err != nil {
		return err
	}

	if stateManager, found := stateManagerFromContext(ctx); found {
		tracked := append([]string{modulesTxtPath, goModPath}, dirs...)
		if res.goSum != nil {
			tracked = append(tracked, goSumPath)
		}
		tracked = append(tracked, res.deleted...)
		for _, c := range copies {
			tracked = append(tracked, c.dst)
		}
		if err = stateManager.TrackAll(tracked...); err != nil {
			return err
		}
	}

	for _, path := range res.deleted {
		if err = removeVendoredFile(moduleDir, path); err != nil {
			return err
		}
	}
	for _, dir := range dirs {
		if err = checkNoSymlink(moduleDir, dir); err != nil {
			return err
		}
		if err = os.MkdirAll(dir, 0o755); err != nil {
			return ex.Wrapf(err, "creating %s", dir)
		}
	}
	for _, c := range copies {
		if err = copyNewFile(scratchVendor, c.src, moduleDir, c.dst); err != nil {
			return err
		}
	}
	// The mode of an existing modules.txt is kept (see util.WriteFileAtomic).
	if err = util.WriteFileAtomic(modulesTxtPath, res.merged.Format()); err != nil {
		return ex.Wrapf(err, "writing %s", modulesTxtPath)
	}
	if err = writeGoMod(goModPath, res.goMod); err != nil {
		return ex.Wrapf(err, "writing %s", goModPath)
	}
	if res.goSum != nil {
		if err = util.WriteFileAtomic(goSumPath, res.goSum); err != nil {
			return ex.Wrapf(err, "writing %s", goSumPath)
		}
	}

	keepForDebug(ctx, goModPath, "")
	util.LoggerFromContext(ctx).InfoContext(ctx, "added the packages of the hook code to the vendor directory",
		"vendor", vendorDir, "packages", len(res.added), "files", len(copies), "removed", len(res.deleted))
	return nil
}

// scratchImports is the result of vendorScratchImports.
type scratchImports struct {
	// packages are the sorted packages that the scratch module imports.
	packages []string
	// toolImports are the imports of otel.instrumentation.go and of the
	// tool files of the instrumentation modules that it imports. They are
	// the instrumentation packages, like in walkInstrumentation.
	toolImports map[string]bool
}

// vendorScratchImports returns the packages that the scratch module imports.
// It walks the instrumentation packages like walkInstrumentation: the imports
// of otel.instrumentation.go, and the imports of the tool file in the module
// of each instrumentation package. The packages are:
//   - the instrumentation packages.
//   - the packages that their rules name (see collectRulePackages).
//
// The scratch module cannot import a package of the main module (the module
// of the user). Such a package (local instrumentation) is replaced by the
// packages that it imports (see expandMainPackages).
func vendorScratchImports(imports map[string]bool, sources *hookSources) (*scratchImports, error) {
	set := make(map[string]bool, len(imports))
	toolImports := maps.Clone(imports)
	visited := make(map[string]bool, len(imports))
	seenToolFiles := make(map[string]bool)
	p := ast.NewAstParser()
	for queue := slices.Sorted(maps.Keys(imports)); len(queue) > 0; {
		importPath := queue[0]
		queue = queue[1:]
		if visited[importPath] {
			continue
		}
		visited[importPath] = true
		set[importPath] = true

		src, ok := sources.packageDir(importPath)
		if !ok {
			continue
		}
		ruleFiles, err := rulesFromDir(src.dir, true)
		if err != nil {
			return nil, err
		}
		for _, ruleFile := range ruleFiles {
			if err = collectRulePackages(ruleFile, set, sources); err != nil {
				return nil, err
			}
		}

		toolFile, err := findToolFile(src.moduleDir)
		if err != nil {
			return nil, err
		}
		if toolFile == "" || seenToolFiles[toolFile] {
			continue
		}
		seenToolFiles[toolFile] = true
		f, err := p.Parse(toolFile, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		nested, err := collectImports(toolFile, f, make(map[string]bool))
		if err != nil {
			return nil, err
		}
		for _, imp := range nested {
			toolImports[imp] = true
			queue = append(queue, imp)
		}
	}
	if err := sources.expandMainPackages(set); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(set))
	for p := range set {
		if !isStdImportPath(p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return &scratchImports{packages: out, toolImports: toolImports}, nil
}

// isStdImportPath reports whether p is a package of the standard library: the
// first element of its path has no dot.
func isStdImportPath(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

// collectRulePackages adds to set the packages that the rule file names: the
// hook packages of the function rules ("path"), the packages of the files
// that the file rules add ("path") and the imports of these files, and the
// values of the "imports" maps of all the rules. The other fields of the
// rules do not name packages: their code uses the packages of "imports".
func collectRulePackages(ruleFile string, set map[string]bool, sources *hookSources) error {
	data, err := os.ReadFile(ruleFile)
	if err != nil {
		return ex.Wrapf(err, "reading rule file %s", ruleFile)
	}
	doc, err := rule.ParseFile(data)
	if err != nil {
		return ex.Wrapf(err, "parsing rule file %s", ruleFile)
	}
	rules, err := doc.Rules()
	if err != nil {
		return ex.Wrapf(err, "parsing rule file %s", ruleFile)
	}
	for _, r := range rules {
		if base := ruleBase(r); base != nil {
			for _, p := range base.Imports {
				if p != "" {
					set[p] = true
				}
			}
		}
		switch rt := r.(type) {
		case *rule.InstFuncRule:
			set[rt.Path] = true
		case *rule.InstFileRule:
			set[rt.Path] = true
			if err = collectFileImports(rt.Path, rt.File, set, sources); err != nil {
				return err
			}
		}
	}
	return nil
}

// ruleBase returns the base rule of r, or nil for an unknown rule type.
func ruleBase(r rule.InstRule) *rule.InstBaseRule {
	switch rt := r.(type) {
	case *rule.InstFuncRule:
		return &rt.InstBaseRule
	case *rule.InstFileRule:
		return &rt.InstBaseRule
	case *rule.InstRawRule:
		return &rt.InstBaseRule
	case *rule.InstStructRule:
		return &rt.InstBaseRule
	case *rule.InstCallRule:
		return &rt.InstBaseRule
	case *rule.InstLitRule:
		return &rt.InstBaseRule
	case *rule.InstDirectiveRule:
		return &rt.InstBaseRule
	case *rule.InstDeclRule:
		return &rt.InstBaseRule
	}
	return nil
}

// collectFileImports adds to set the imports of the file that a file rule
// adds. The file is in the package importPath.
func collectFileImports(importPath, file string, set map[string]bool, sources *hookSources) error {
	src, ok := sources.packageDir(importPath)
	if !ok {
		return nil
	}
	path := filepath.Join(src.dir, file)
	if !util.PathExists(path) {
		return nil
	}
	imports, err := goFileImports(path)
	if err != nil {
		return err
	}
	for _, p := range imports {
		set[p] = true
	}
	return nil
}

// goFileImports returns the imports of the Go file at path.
func goFileImports(path string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, ex.Wrapf(err, "parsing the imports of %s", path)
	}
	imports := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		p, unquoteErr := strconv.Unquote(imp.Path.Value)
		if unquoteErr != nil {
			return nil, ex.Wrapf(unquoteErr, "parsing the imports of %s", path)
		}
		imports = append(imports, p)
	}
	return imports, nil
}

// hookSources finds the source files of the packages of the hook code. A
// package can be:
//   - in the extracted otelc bundle.
//   - in the main module (the module of the user).
//   - in a vendored module that go.mod replaces with a local directory: the
//     files are in this directory, also the files that vendor/ does not have.
//   - in a different vendored module: the files are in vendor/.
type hookSources struct {
	// moduleDir is the directory of the main module.
	moduleDir string
	// mainPath is the module path of the main module.
	mainPath string
	// user is the vendor/modules.txt of the user.
	user *modulesTxt
	// localDirs are the vendored modules that go.mod replaces with a local
	// directory, and their absolute directories (see localModuleDirs).
	localDirs map[string]string
}

// packageSource are the directories of a package.
type packageSource struct {
	// dir is the directory of the package.
	dir string
	// moduleDir is the root directory of its module.
	moduleDir string
}

// owner returns the path of the module (the main module, or a vendored
// module) that has the package importPath. When two modules can have it, the
// longest module path wins, like in the go command. vendored is nil for the
// main module. The path is "" when no module can have the package.
func (h *hookSources) owner(importPath string) (string, *vendorModule) {
	modPath := ""
	var vendored *vendorModule
	if inModule(importPath, h.mainPath) {
		modPath = h.mainPath
	}
	for _, mod := range h.user.usedModules() {
		if len(mod.Path) > len(modPath) && inModule(importPath, mod.Path) {
			modPath, vendored = mod.Path, mod
		}
	}
	return modPath, vendored
}

// inModule reports whether the package importPath can be in the module
// modPath.
func inModule(importPath, modPath string) bool {
	return modPath != "" && (importPath == modPath || strings.HasPrefix(importPath, modPath+"/"))
}

// isMainPackage reports whether importPath is a package of the main module.
func (h *hookSources) isMainPackage(importPath string) bool {
	modPath, vendored := h.owner(importPath)
	return modPath != "" && vendored == nil
}

// packageDir returns the directories of the package importPath. ok is false
// when otelc does not find the package directory. The import paths come from
// rule files and Go files, so an import path that is not valid (for example
// with "..") is not used to make a file path.
func (h *hookSources) packageDir(importPath string) (packageSource, bool) {
	if module.CheckImportPath(importPath) != nil {
		return packageSource{}, false
	}
	if dir, ok := embeddedSourceDir(importPath); ok {
		modDir, found := enclosingModuleDir(dir)
		if !found {
			modDir = dir
		}
		return packageSource{dir: dir, moduleDir: modDir}, true
	}
	modPath, vendored := h.owner(importPath)
	var root string
	switch {
	case modPath == "":
		return packageSource{}, false
	case vendored == nil:
		root = h.moduleDir
	case h.localDirs[modPath] != "":
		root = h.localDirs[modPath]
	default:
		root = filepath.Join(h.moduleDir, vendorDirName, filepath.FromSlash(modPath))
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(importPath, modPath), "/")
	dir := filepath.Join(root, filepath.FromSlash(rel))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return packageSource{}, false
	}
	return packageSource{dir: dir, moduleDir: root}, true
}

// expandMainPackages replaces each package of the main module in set with
// the packages that it imports. The imports of a package of the main module
// are expanded too. The scratch module cannot import the packages of the main
// module, but it must have their imports: for example the otelc hook API that
// a local hook package imports. The main module is not in vendor/, so it
// never goes into vendor/modules.txt or into the require directives.
func (h *hookSources) expandMainPackages(set map[string]bool) error {
	var queue []string
	for p := range set {
		if h.isMainPackage(p) {
			queue = append(queue, p)
		}
	}
	slices.Sort(queue)
	visited := make(map[string]bool)
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		delete(set, pkg)
		if visited[pkg] {
			continue
		}
		visited[pkg] = true
		imports, err := packageImports(filepath.Join(h.moduleDir,
			filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(pkg, h.mainPath), "/"))))
		if err != nil {
			return err
		}
		for _, imp := range imports {
			// A module path can have no dot (for example "myapp"), so the
			// main module is checked before the standard library.
			switch {
			case h.isMainPackage(imp):
				queue = append(queue, imp)
			case isStdImportPath(imp):
			default:
				set[imp] = true
			}
		}
	}
	return nil
}

// packageImports returns the imports of the Go files of the package in dir.
// Like "go mod vendor", it uses the files of all the platforms and all the
// build tags, but not the test files and not the files with the "ignore"
// build tag. A directory that does not exist has no imports.
func packageImports(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, ex.Wrapf(err, "reading %s", dir)
	}
	var imports []string
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
			strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		path := filepath.Join(dir, name)
		f, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.ParseComments)
		if parseErr != nil {
			return nil, ex.Wrapf(parseErr, "parsing the imports of %s", path)
		}
		if ignoredGoFile(f) {
			continue
		}
		for _, imp := range f.Imports {
			p, unquoteErr := strconv.Unquote(imp.Path.Value)
			if unquoteErr != nil {
				return nil, ex.Wrapf(unquoteErr, "parsing the imports of %s", path)
			}
			imports = append(imports, p)
		}
	}
	return imports, nil
}

// ignoredGoFile reports whether the build constraints of f exclude f for all
// the build tags but "ignore", like "go mod vendor" does. When f has a
// //go:build line, only this line counts. Else the "// +build" lines count,
// and f must match all of them. As for the go command, a "// +build" line in
// the doc comment of the package clause is not a build constraint.
func ignoredGoFile(f *goast.File) bool {
	var plusBuild []constraint.Expr
	for _, group := range f.Comments {
		if group.Pos() > f.Package {
			break
		}
		for _, c := range group.List {
			switch {
			case constraint.IsGoBuild(c.Text):
				expr, err := constraint.Parse(c.Text)
				if err != nil {
					// The go command reports the bad line.
					return false
				}
				return !matchAnyTag(expr, true)
			case constraint.IsPlusBuild(c.Text) && group != f.Doc:
				// Like the go command, otelc does not use a bad
				// "// +build" line.
				if expr, err := constraint.Parse(c.Text); err == nil {
					plusBuild = append(plusBuild, expr)
				}
			}
		}
	}
	for _, expr := range plusBuild {
		if !matchAnyTag(expr, true) {
			return true
		}
	}
	return false
}

// matchAnyTag evaluates expr like the go command does for "go mod vendor":
// each build tag, except "ignore", has the value that makes the file match
// (prefer). A negation inverts prefer.
func matchAnyTag(expr constraint.Expr, prefer bool) bool {
	switch x := expr.(type) {
	case *constraint.TagExpr:
		return x.Tag != "ignore" && prefer
	case *constraint.NotExpr:
		return !matchAnyTag(x.X, !prefer)
	case *constraint.AndExpr:
		return matchAnyTag(x.X, prefer) && matchAnyTag(x.Y, prefer)
	case *constraint.OrExpr:
		return matchAnyTag(x.X, prefer) || matchAnyTag(x.Y, prefer)
	}
	return false
}

// localModuleDirs returns the vendored modules of user that go.mod replaces
// with a local directory (for example "replace example.com/hooks =>
// ./hooks"), and the absolute path of the directory. A relative directory is
// relative to moduleDir. A directory is returned only when its go.mod file
// has the module path: else otelc uses the vendored files.
func localModuleDirs(user *modulesTxt, moduleDir string) map[string]string {
	dirs := make(map[string]string)
	for _, mod := range user.usedModules() {
		if !isDirReplacement(mod.Replacement) {
			continue
		}
		dir := filepath.FromSlash(mod.Replacement)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(moduleDir, dir)
		}
		mf, err := parseGoMod(filepath.Join(dir, goModFileName))
		if err != nil || mf.Module == nil || mf.Module.Mod.Path != mod.Path {
			continue
		}
		dirs[mod.Path] = dir
	}
	return dirs
}

// isDirReplacement reports whether the replacement text of vendor/modules.txt
// is a local directory. A module replacement has a version after the path.
func isDirReplacement(replacement string) bool {
	return replacement != "" && !strings.Contains(replacement, " ") && modfile.IsDirectoryPath(replacement)
}

// embeddedSourceDir returns the directory of the package importPath in the
// extracted otelc bundle, when importPath is a package of an otelc module and
// the directory exists.
func embeddedSourceDir(importPath string) (string, bool) {
	// An import path can come from a rule file. A valid import path has no
	// ".." element, so dir is always below the extracted bundle.
	if module.CheckImportPath(importPath) != nil {
		return "", false
	}
	var dir string
	switch {
	case importPath == util.OtelcInstRoot:
		dir = filepath.Join(util.GetBuildTempDir(), unzippedInstDir)
	case strings.HasPrefix(importPath, util.OtelcInstRoot+"/"):
		dir = filepath.Join(util.GetBuildTempDir(), unzippedInstDir,
			filepath.FromSlash(strings.TrimPrefix(importPath, util.OtelcInstRoot+"/")))
	case importPath == util.OtelcPkgRoot:
		dir = filepath.Join(util.GetBuildTempDir(), unzippedPkgDir)
	case strings.HasPrefix(importPath, util.OtelcPkgRoot+"/"):
		dir = filepath.Join(util.GetBuildTempDir(), unzippedPkgDir,
			filepath.FromSlash(strings.TrimPrefix(importPath, util.OtelcPkgRoot+"/")))
	default:
		return "", false
	}
	info, err := os.Stat(dir) //nolint:gosec // dir is below the bundle: importPath is valid (see above)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return dir, true
}

// isInVendorDir reports whether dir is inside a vendor directory.
func isInVendorDir(dir string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(dir), "/"), vendorDirName)
}

// maxScratchRounds limits the number of times that buildVendorScratch changes
// the way it gets the vendored modules.
const maxScratchRounds = 10

// roundsPerVendoredModule is the number of additional rounds for each
// vendored module: a module can be updated, then pinned.
const roundsPerVendoredModule = 2

// vendorScratchInput is the input of buildVendorScratch.
type vendorScratchInput struct {
	// user is the vendor/modules.txt of the user, without the modules that
	// are replaced (replacedMods).
	user *modulesTxt
	// original is the full vendor/modules.txt of the user.
	original *modulesTxt
	// replacedMods are vendored otelc modules that are replaced by the
	// modules of the bundle. The scratch module imports their vendored
	// packages too, so that vendor/ keeps them.
	replacedMods map[string]bool
	// userVendorDir is the vendor directory of the user.
	userVendorDir string
	// imports are the packages that the scratch module imports.
	imports []string
	// replaces are the otelc modules and their directories in the bundle.
	replaces map[string]string
	// localDirs are the vendored modules that go.mod replaces with a local
	// directory, and their absolute directories. The scratch module uses
	// these directories, not a copy of vendor/.
	localDirs map[string]string
	// goVersion is the go version of the module of the user.
	goVersion string
	// update permits a higher version of a vendored module, when the hook
	// code needs it. Without update, this is a *vendorConflictError.
	update bool
}

// vendorScratch is the state of the scratch module.
type vendorScratch struct {
	// dir is the directory of the scratch module.
	dir string
	// userMods are the vendored modules of the user that the scratch module
	// requires.
	userMods map[string]bool
	// versions are the versions that the scratch module requires for
	// userMods: the vendored version, or a higher one for an updated module.
	versions map[string]string
	// copies are the relative directories of the copies of userMods.
	copies map[string]string
	// pinned are the vendored modules that the scratch module gets at their
	// vendored version, because their copy does not have all the packages.
	pinned map[string]bool
	// updated are the vendored modules that the scratch module gets at a
	// higher version (only with vendorScratchInput.update).
	updated map[string]bool
	// extra are packages of the user that the scratch module imports too:
	// the vendored packages of the updated and the replaced modules.
	extra map[string]bool
}

// fixedMods returns the vendored modules of the user that keep their vendored
// version.
func (s *vendorScratch) fixedMods() map[string]bool {
	fixed := maps.Clone(s.userMods)
	for path := range s.updated {
		delete(fixed, path)
	}
	return fixed
}

// notCopied returns the vendored modules that do not use their copy.
func (s *vendorScratch) notCopied() map[string]bool {
	out := maps.Clone(s.pinned)
	maps.Copy(out, s.updated)
	return out
}

// imports returns the sorted packages that the scratch module imports.
func (s *vendorScratch) imports(hookImports []string) []string {
	set := make(map[string]bool, len(hookImports)+len(s.extra))
	for _, p := range hookImports {
		set[p] = true
	}
	maps.Copy(set, s.extra)
	return slices.Sorted(maps.Keys(set))
}

// addExtra adds the vendored packages of the module path in m to the imports
// of the scratch module.
func (s *vendorScratch) addExtra(m *modulesTxt, path string) {
	if mod := m.find(path); mod != nil {
		for _, pkg := range mod.Packages {
			s.extra[pkg] = true
		}
	}
}

// buildVendorScratch makes the scratch module, and runs "go mod tidy" and
// "go mod vendor" in it.
//
// The scratch module first replaces each vendored module with a copy of its
// vendored packages, so that the go command does not download it. When the
// hook code needs a package of a vendored module that vendor/ does not have,
// the copy is not sufficient: the scratch module then gets this module at the
// vendored version (from the module cache or the network), and tries again.
// When the hook code needs a higher version of a vendored module, it is a
// conflict, or (with update) the scratch module gets the higher version.
func buildVendorScratch(ctx context.Context, in vendorScratchInput) (*vendorScratch, error) {
	dir := util.GetBuildTemp(vendorScratchDir)
	if err := os.RemoveAll(dir); err != nil {
		return nil, ex.Wrapf(err, "removing the old scratch module %s", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, ex.Wrapf(err, "creating the scratch module %s", dir)
	}

	userMods, copies, err := copyVendoredModules(dir, in)
	if err != nil {
		return nil, err
	}
	s := &vendorScratch{
		dir:      dir,
		userMods: userMods,
		versions: make(map[string]string, len(userMods)),
		copies:   copies,
		pinned:   make(map[string]bool),
		updated:  make(map[string]bool),
		extra:    make(map[string]bool),
	}
	for path := range userMods {
		s.versions[path], _ = in.user.Module(path)
	}
	for path := range in.replacedMods {
		s.addExtra(in.original, path)
	}

	for round := 0; ; round++ {
		done, roundErr := runScratchRound(ctx, s, in)
		if roundErr != nil {
			return nil, roundErr
		}
		if done {
			return s, nil
		}
		// Each round makes progress: it updates a module to a higher
		// version, pins a module, or drops a package. A long chain of
		// updates needs about one round for each vendored module.
		if limit := maxScratchRounds + roundsPerVendoredModule*len(s.userMods); round >= limit {
			return nil, ex.Newf("cannot resolve the modules of the otelc hook code in %d rounds", limit)
		}
	}
}

// copyVendoredModules copies each vendored module of the user (but not the
// otelc modules, and not the modules in in.localDirs) into the scratch module
// in dir. It returns the vendored modules, and the relative directory of each
// copy.
func copyVendoredModules(dir string, in vendorScratchInput) (map[string]bool, map[string]string, error) {
	userMods := make(map[string]bool)
	copies := make(map[string]string)
	moduleDir := filepath.Dir(in.userVendorDir)
	copied := in.original
	if copied == nil {
		copied = in.user
	}
	own := vendoredDirsOf(copied)
	for i, mod := range in.user.usedModules() {
		// A module without packages is copied too (only its go.mod file), so
		// that the go command does not download its go.mod file. A used
		// module always has a version (see usedModules).
		if _, isOtelc := in.replaces[mod.Path]; isOtelc {
			// The scratch module takes the otelc modules from the bundle.
			continue
		}
		if in.localDirs[mod.Path] != "" {
			// The scratch module takes the module from its local directory.
			userMods[mod.Path] = true
			continue
		}
		rel := filepath.Join(vendorScratchDepsDir, fmt.Sprintf("%04d", i))
		if err := copyVendoredModule(mod, moduleDir, dir, filepath.Join(dir, rel), own); err != nil {
			return nil, nil, err
		}
		userMods[mod.Path] = true
		copies[mod.Path] = "./" + filepath.ToSlash(rel)
	}
	return userMods, copies, nil
}

// runScratchRound writes the scratch module and runs the go commands. It
// changes s when the result shows that a different way to get a vendored
// module is necessary, and then returns false. It returns true when the
// vendor directory of the scratch module is complete.
func runScratchRound(ctx context.Context, s *vendorScratch, in vendorScratchInput) (bool, error) {
	logger := util.LoggerFromContext(ctx)
	if err := writeScratchSource(s.dir, s.imports(in.imports)); err != nil {
		return false, err
	}
	if err := writeScratchGoMod(s, in); err != nil {
		return false, err
	}
	if err := writeScratchGoSum(filepath.Dir(in.userVendorDir), s.dir); err != nil {
		return false, err
	}
	// -e continues after errors (for example a package that is not in a
	// vendored copy), so that the checks below can find the cause.
	tidy, tidyErr := scratchGo(ctx, s.dir, "mod", "tidy", "-e")
	pkgs, conflicts, err := checkScratchConflicts(ctx, s)
	if err != nil {
		return false, err
	}
	failed := missingPackagesInOutput(tidy.stderr)
	for _, p := range pkgs {
		if p.failed && p.module == "" {
			failed = append(failed, p.importPath)
		}
	}

	// A package of the user that the new version of a module does not have
	// anymore is not imported. The go build reports it if the user needs it.
	var dropped []string
	for _, p := range failed {
		if s.extra[p] {
			delete(s.extra, p)
			dropped = append(dropped, p)
		}
	}
	if len(dropped) > 0 {
		logger.WarnContext(ctx, "the new version of a vendored module does not have some vendored packages",
			"packages", dropped)
		return false, nil
	}

	if len(conflicts) > 0 {
		if !in.update {
			return false, conflictsError(conflicts)
		}
		return false, s.update(conflicts, in.user)
	}
	if in.update {
		// otelc vendor changes go.mod for all time, so go.mod must agree with
		// minimal version selection, like after "go get": else a later
		// "go mod vendor" or "go mod tidy" fails. All the vendored modules
		// that the module graph selects at a higher version are updated,
		// also when no package of the hook code imports them.
		raised, raisedErr := raisedModules(ctx, s)
		if raisedErr != nil {
			return false, raisedErr
		}
		if len(raised) > 0 {
			return false, s.update(raised, in.user)
		}
	}

	if missing := copiedModulesWithMissingPackages(failed, s.copies, s.notCopied()); len(missing) > 0 {
		for _, path := range missing {
			if pinErr := s.pin(ctx, path, failed); pinErr != nil {
				return false, pinErr
			}
		}
		return false, nil
	}

	if _, err = scratchGo(ctx, s.dir, "mod", "vendor"); err != nil {
		if tidyErr != nil {
			err = ex.Wrapf(err, "go mod tidy reported:\n%s", tidy.stderr)
		}
		return false, err
	}
	return true, nil
}

// raisedModules returns the vendored modules of the user that keep their
// vendored version, but that the module graph of the scratch module selects
// at a higher version.
func raisedModules(ctx context.Context, s *vendorScratch) ([]*vendorConflictError, error) {
	out, err := scratchGo(ctx, s.dir, "list", "-m", "-e", "-f", "{{.Path}} {{.Version}}", "all")
	if err != nil {
		return nil, err
	}
	fixed := s.fixedMods()
	var raised []*vendorConflictError
	for line := range strings.Lines(string(out.stdout)) {
		path, selected, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || !fixed[path] || semver.Compare(selected, s.versions[path]) <= 0 {
			continue
		}
		raised = append(raised, &vendorConflictError{
			Module:          path,
			VendoredVersion: s.versions[path],
			RequiredVersion: selected,
			RequiredBy:      "the module graph",
		})
	}
	return raised, nil
}

// conflictsError returns the error of the conflicts: one error for each
// module, with its highest required version.
func conflictsError(conflicts []*vendorConflictError) error {
	var errs []error
	seen := make(map[string]bool)
	for _, c := range conflicts {
		if !seen[c.Module] {
			seen[c.Module] = true
			errs = append(errs, c)
		}
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return ex.Join(errs...)
}

// update makes the scratch module get the required version of the modules of
// the conflicts. A module that the user replaces cannot be updated.
func (s *vendorScratch) update(conflicts []*vendorConflictError, user *modulesTxt) error {
	for _, c := range conflicts {
		if mod := user.find(c.Module); mod != nil && mod.Replacement != "" {
			return ex.Wrapf(c, "otelc vendor cannot update %s, because go.mod replaces it with %s",
				c.Module, mod.Replacement)
		}
		if semver.Compare(c.RequiredVersion, s.versions[c.Module]) <= 0 {
			continue
		}
		s.versions[c.Module] = c.RequiredVersion
		s.updated[c.Module] = true
		delete(s.pinned, c.Module)
		s.addExtra(user, c.Module)
	}
	return nil
}

// pin makes the scratch module get the vendored module path at its vendored
// version, because the copy of vendor/ misses packages that the hook code
// needs. It first downloads the module (or finds it in the module cache), so
// that a failure tells which module and packages are the cause.
func (s *vendorScratch) pin(ctx context.Context, path string, failed []string) error {
	version := s.versions[path]
	var pkgs []string
	for _, p := range failed {
		if p == path || strings.HasPrefix(p, path+"/") {
			pkgs = append(pkgs, p)
		}
	}
	slices.Sort(pkgs)
	pkgs = slices.Compact(pkgs)
	util.LoggerFromContext(ctx).InfoContext(ctx,
		"the hook code needs packages of a vendored module that vendor/ does not have; "+
			"getting the module at its vendored version", "module", path, "version", version, "packages", pkgs)
	if _, err := scratchGo(ctx, s.dir, "mod", "download", path+"@"+version); err != nil {
		return &vendorFetchError{Module: path, Version: version, Packages: pkgs, Err: err}
	}
	s.pinned[path] = true
	return nil
}

// vendorFetchError tells that the hook code needs packages of a vendored
// module that vendor/ does not have, and that otelc cannot get the module at
// the vendored version.
type vendorFetchError struct {
	Module   string
	Version  string
	Packages []string
	Err      error
}

func (e *vendorFetchError) Error() string {
	return fmt.Sprintf(
		"the otelc hook code needs the packages %s of module %s@%s, but vendor/ does not have them, "+
			"and otelc cannot download %s@%s. To fix it, add them to vendor/ "+
			"(import them in your module, then run \"go mod vendor\"), or run \"otelc vendor\" "+
			"with network access, and build again: %v",
		strings.Join(e.Packages, ", "), e.Module, e.Version, e.Module, e.Version, e.Err)
}

func (e *vendorFetchError) Unwrap() error { return e.Err }

// writeScratchSource writes the Go file of the scratch module, which imports
// the packages of the hook code.
func writeScratchSource(dir string, imports []string) error {
	header := []string{
		"// Code generated by otelc. DO NOT EDIT.",
		"",
		"// Package vendorscratch imports the packages of the otelc hook code.",
		"package vendorscratch",
		"",
		"import (",
	}
	footer := []string{")", ""}
	lines := make([]string, 0, len(header)+len(imports)+len(footer))
	lines = append(lines, header...)
	for _, p := range imports {
		lines = append(lines, "\t_ "+strconv.Quote(p))
	}
	lines = append(lines, footer...)
	src := []byte(strings.Join(lines, "\n"))
	return util.WriteFileAtomic(filepath.Join(dir, vendorScratchFile), src, vendorFilePerm)
}

// writeScratchGoMod writes the go.mod file of the scratch module. It requires
// each module of the user at its version in s. A vendored module is replaced
// by its copy, or, when it is pinned, by the same module at the vendored
// version. A module that go.mod replaces with a local directory is replaced
// by this directory. The replacements apply to all the versions, so the
// vendored version is always used. An updated module is not replaced. The
// otelc modules come from the bundle.
func writeScratchGoMod(s *vendorScratch, in vendorScratchInput) error {
	mf, err := modfile.Parse(goModFileName, []byte("module "+vendorScratchModule+"\n"), nil)
	if err != nil {
		return ex.Wrap(err)
	}
	if in.goVersion != "" {
		if err = mf.AddGoStmt(in.goVersion); err != nil {
			return ex.Wrap(err)
		}
	}
	for _, mod := range in.user.usedModules() {
		if _, isOtelc := in.replaces[mod.Path]; isOtelc {
			continue
		}
		version := mod.Version
		if v := s.versions[mod.Path]; v != "" {
			version = v
		}
		if err = mf.AddRequire(mod.Path, version); err != nil {
			return ex.Wrap(err)
		}
		switch {
		case s.updated[mod.Path]:
			// The go command gets the higher version.
		case s.pinned[mod.Path]:
			err = mf.AddReplace(mod.Path, "", mod.Path, mod.Version)
		case in.localDirs[mod.Path] != "":
			err = mf.AddReplace(mod.Path, "", in.localDirs[mod.Path], "")
		case s.copies[mod.Path] != "":
			err = mf.AddReplace(mod.Path, "", s.copies[mod.Path], "")
		}
		if err != nil {
			return ex.Wrap(err)
		}
	}
	for _, oldPath := range slices.Sorted(maps.Keys(in.replaces)) {
		if err = mf.AddReplace(oldPath, "", in.replaces[oldPath], ""); err != nil {
			return ex.Wrap(err)
		}
	}
	return writeGoMod(filepath.Join(s.dir, goModFileName), mf)
}

// missingPackageRegexp finds the packages that the go command cannot find in
// its error messages, for example:
//
//	x/y: no required module provides package x/y; to add it: ...
//	x/y: module x@latest found (v1.0.0, replaced by ./d), but does not contain package x/y
var missingPackageRegexp = regexp.MustCompile(`(?:provides package|does not contain package) ([^\s;:,]+)`)

// missingPackagesInOutput returns the packages that the go command reports as
// not found in out. `go mod tidy` looks at the files of all the platforms, so
// its output finds more packages than `go list` for the current platform.
func missingPackagesInOutput(out []byte) []string {
	matches := missingPackageRegexp.FindAllSubmatch(out, -1)
	pkgs := make([]string, 0, len(matches))
	for _, m := range matches {
		pkgs = append(pkgs, string(m[1]))
	}
	return pkgs
}

// copiedModulesWithMissingPackages returns the sorted modules that are
// replaced by a copy of vendor/, but that do not have one of the failed
// packages that the hook code imports. The modules in notCopied do not use
// their copy, so they are not returned.
func copiedModulesWithMissingPackages(failed []string, copies map[string]string, notCopied map[string]bool) []string {
	found := make(map[string]bool)
	for _, importPath := range failed {
		owner := ""
		for path := range copies {
			if len(path) <= len(owner) {
				continue
			}
			if importPath == path || strings.HasPrefix(importPath, path+"/") {
				owner = path
			}
		}
		if owner != "" && !notCopied[owner] {
			found[owner] = true
		}
	}
	return slices.Sorted(maps.Keys(found))
}

// checkScratchConflicts finds the vendored modules of the user that the hook
// code needs at a higher version, with findVendorConflicts. It returns the
// packages of the scratch module and the conflicts.
func checkScratchConflicts(ctx context.Context, s *vendorScratch) ([]listedPackage, []*vendorConflictError, error) {
	graph, err := scratchGo(ctx, s.dir, "mod", "graph")
	if err != nil {
		return nil, nil, err
	}
	const listFormat = "{{.ImportPath}}\t{{with .Module}}{{.Path}}@{{.Version}}{{end}}\t" +
		"{{join .Imports \" \"}}\t{{if .Error}}error{{end}}"
	list, err := scratchGo(ctx, s.dir, "list", "-e", "-deps", "-f", listFormat, ".")
	if err != nil {
		return nil, nil, err
	}
	pkgs := parseListedPackages(list.stdout)
	conflicts := findVendorConflicts(s.versions, s.fixedMods(), parseModGraph(graph.stdout), pkgs)
	return pkgs, conflicts, nil
}

// listedPackage is one package of `go list -deps`.
type listedPackage struct {
	importPath string
	// module is "path@version", or "" for a package of the standard library.
	module  string
	imports []string
	// failed is true when the go command cannot load the package.
	failed bool
}

func parseListedPackages(out []byte) []listedPackage {
	var pkgs []listedPackage
	for line := range strings.Lines(string(out)) {
		fields := strings.Split(strings.TrimRight(line, "\r\n"), "\t")
		const listFields = 4
		if len(fields) != listFields || fields[0] == "" {
			continue
		}
		pkgs = append(pkgs, listedPackage{
			importPath: fields[0],
			module:     fields[1],
			imports:    strings.Fields(fields[2]),
			failed:     fields[3] != "",
		})
	}
	return pkgs
}

// parseModGraph parses the output of `go mod graph`. It returns, for each
// "path@version" node, the versions of the modules that it requires.
func parseModGraph(out []byte) map[string]map[string]string {
	graph := make(map[string]map[string]string)
	for line := range strings.Lines(string(out)) {
		from, to, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		path, version, ok := strings.Cut(to, "@")
		if !ok {
			continue
		}
		if graph[from] == nil {
			graph[from] = make(map[string]string)
		}
		graph[from][path] = version
	}
	return graph
}

// findVendorConflicts finds the vendored modules of the user that the hook
// code needs at a higher version than the one in versions (the vendored
// version, or the version of an updated module).
//
// The go.mod file of a module lists many modules that it does not import
// itself (they are there for the module graph). So a requirement counts only
// when a package of the requiring module imports a package of the vendored
// module. A vendored module of the user that keeps its vendored version
// (fixed) does not count as the requiring module: the user resolved it
// already, together with its dependencies.
//
// All the requiring modules count: the otelc modules (the hook code itself)
// and their dependencies. The vendored code must not be older than the code
// that the hook code was made for.
func findVendorConflicts(
	versions map[string]string,
	fixed map[string]bool,
	graph map[string]map[string]string,
	pkgs []listedPackage,
) []*vendorConflictError {
	moduleOf := make(map[string]string, len(pkgs))
	for _, p := range pkgs {
		moduleOf[p.importPath] = p.module
	}

	var conflicts []*vendorConflictError
	seen := make(map[string]bool)
	for _, p := range pkgs {
		fromPath, _, _ := strings.Cut(p.module, "@")
		if p.module == "" || fromPath == vendorScratchModule || fixed[fromPath] {
			continue
		}
		for _, imp := range p.imports {
			toPath, _, _ := strings.Cut(moduleOf[imp], "@")
			if toPath == "" || toPath == fromPath {
				continue
			}
			vendored := versions[toPath]
			required := graph[p.module][toPath]
			if vendored == "" || required == "" || semver.Compare(required, vendored) <= 0 {
				continue
			}
			key := toPath + " " + required + " " + fromPath
			if seen[key] {
				continue
			}
			seen[key] = true
			conflicts = append(conflicts, &vendorConflictError{
				Module:          toPath,
				VendoredVersion: vendored,
				RequiredVersion: required,
				RequiredBy:      fromPath,
			})
		}
	}
	// Sort by module path, then by the highest required version, so that the
	// first one tells the fix for its module.
	slices.SortFunc(conflicts, func(a, b *vendorConflictError) int {
		if c := strings.Compare(a.Module, b.Module); c != 0 {
			return c
		}
		if c := semver.Compare(b.RequiredVersion, a.RequiredVersion); c != 0 {
			return c
		}
		return strings.Compare(a.RequiredBy, b.RequiredBy)
	})
	return conflicts
}

// isOtelcModule reports whether path is an otelc module.
func isOtelcModule(path string) bool {
	return path == util.OtelcRoot || strings.HasPrefix(path, util.OtelcRoot+"/")
}

// atVendoredVersions returns a copy of injected (the vendor/modules.txt of
// the scratch module) where each module of userMods has the vendored version
// of the user and no replacement: the scratch module took it at this version.
func atVendoredVersions(injected, user *modulesTxt, userMods map[string]bool) *modulesTxt {
	out := injected.clone()
	for _, mod := range out.modules {
		if !userMods[mod.Path] || mod.trailing() {
			continue
		}
		if version, ok := user.Module(mod.Path); ok {
			mod.Version = version
			mod.Replacement = ""
		}
	}
	return out
}

// copyVendoredModule copies the vendored packages of mod to dst, and writes a
// go.mod file for it. The scratch module replaces mod with this copy. The
// files come from the vendor directory of the module in moduleDir, and dst is
// in the scratch module in scratchDir. own are the vendored packages and
// module roots of the user (see packageFiles).
//
// On Linux, the files are hard links to the files of vendor/ when possible
// (see linkNewFile): a copy of a large vendor/ takes time. Only the go
// command reads the files of the copy, and the go.mod file is a new file.
func copyVendoredModule(mod *vendorModule, moduleDir, scratchDir, dst string, own vendoredDirs) error {
	userVendorDir := filepath.Join(moduleDir, vendorDirName)
	if err := checkInside(scratchDir, dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return ex.Wrap(err)
	}
	made := map[string]bool{dst: true}
	for _, pkg := range mod.Packages {
		// parseModulesTxt checks that pkg is mod.Path or below it.
		rel := strings.TrimPrefix(strings.TrimPrefix(pkg, mod.Path), "/")
		srcDir := filepath.Join(userVendorDir, filepath.FromSlash(pkg))
		dstDir := filepath.Join(dst, filepath.FromSlash(rel))
		if err := checkInside(dst, dstDir); err != nil {
			return err
		}
		// packageFiles checks srcDir, and returns only regular files below
		// it. dst is a new directory of the scratch module. Thus the files
		// do not need the checks of linkNewFile, which take time for a
		// large vendor/.
		files, err := packageFiles(moduleDir, srcDir, pkg, own)
		if err != nil {
			return err
		}
		for _, src := range files {
			fileRel, relErr := filepath.Rel(srcDir, src)
			if relErr != nil {
				return ex.Wrap(relErr)
			}
			if pkg == mod.Path && fileRel == goModFileName {
				// "go mod vendor" of go < 1.17 copies go.mod. The copy gets
				// a new go.mod file below.
				continue
			}
			target := filepath.Join(dstDir, fileRel)
			if parent := filepath.Dir(target); !made[parent] {
				if err = os.MkdirAll(parent, 0o755); err != nil {
					return ex.Wrap(err)
				}
				made[parent] = true
			}
			if err = linkOrCopyFile(moduleDir, src, scratchDir, target); err != nil {
				return err
			}
		}
	}
	// util.WriteFileAtomic makes a new file (it renames a temporary file), so
	// it never writes through a hard link to vendor/.
	goMod := "module " + mod.Path + "\n"
	if v := mod.goVersion(); v != "" {
		goMod += "\ngo " + v + "\n"
	}
	return util.WriteFileAtomic(filepath.Join(dst, goModFileName), []byte(goMod), vendorFilePerm)
}

// goOutput is the output of a go command.
type goOutput struct {
	stdout []byte
	stderr []byte
}

// scratchGo runs a go command in the scratch module and returns its output.
// The scratch module uses module mode: a -mod flag of the user in GOFLAGS is
// removed, and the workspace mode is disabled. A -modfile flag is removed
// too: else the go command changes the go.mod file of the user, not the one
// of the scratch module.
func scratchGo(ctx context.Context, dir string, args ...string) (goOutput, error) {
	logger := util.LoggerFromContext(ctx)
	goflags := goflagsWithoutModfile(goflagsWithoutMod(util.StripToolexecFromGoflags(os.Getenv("GOFLAGS"))))
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS="+goflags)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	logger.DebugContext(ctx, "ran go in the vendor scratch module",
		"dir", dir, "args", args, "stderr", stderr.String())
	out := goOutput{stdout: stdout.Bytes(), stderr: stderr.Bytes()}
	if err != nil {
		return out, ex.Wrapf(err,
			"resolving the modules of the otelc hook code (go %s in %s) failed:\n%s%s",
			strings.Join(args, " "), dir, stdout.String(), stderr.String())
	}
	return out, nil
}

// updateGoModForVendor adds to mf the require and replace directives of the
// modules that merged has and user does not have. It also changes the
// replacements of these modules in merged to the text of go.mod, so that the
// vendor consistency check of the go command passes.
func updateGoModForVendor(mf *modfile.File, user, merged *modulesTxt, moduleDir string) error {
	maxGo := ""
	if mf.Go != nil {
		maxGo = mf.Go.Version
	}
	for _, mod := range merged.usedModules() {
		if user.find(mod.Path) != nil {
			continue
		}
		// The vendor consistency check needs a require for each module that
		// modules.txt marks explicit.
		if slices.ContainsFunc(mf.Require, func(r *modfile.Require) bool { return r.Mod.Path == mod.Path }) {
			if err := mf.AddRequire(mod.Path, mod.Version); err != nil {
				return ex.Wrap(err)
			}
		} else {
			mf.AddNewRequire(mod.Path, mod.Version, true)
		}

		if v := mod.goVersion(); v != "" && (maxGo == "" || goversion.Compare("go"+v, "go"+maxGo) > 0) {
			maxGo = v
		}

		// A replace of the user wins: the vendor consistency check compares
		// modules.txt with it.
		if text, ok := userReplacement(mf, mod.Path, mod.Version); ok {
			mod.Replacement = text
			continue
		}
		if mod.Replacement == "" {
			continue
		}
		newPath, newVersion, _ := strings.Cut(mod.Replacement, " ")
		if filepath.IsAbs(newPath) {
			newPath = relativeReplacePath(moduleDir, newPath)
			mod.Replacement = newPath
			if newVersion != "" {
				mod.Replacement += " " + newVersion
			}
		}
		if err := mf.AddReplace(mod.Path, mod.Version, newPath, newVersion); err != nil {
			return ex.Wrap(err)
		}
	}

	// The go command uses the go version of the main module as the minimum
	// for all the modules. A module of the hook code can need a higher one.
	if mf.Go != nil && maxGo != "" && goversion.Compare("go"+maxGo, "go"+mf.Go.Version) > 0 {
		if err := mf.AddGoStmt(maxGo); err != nil {
			return ex.Wrap(err)
		}
	}
	mf.Cleanup()
	return nil
}

// userReplacement returns the replacement text (as in vendor/modules.txt) of
// a replace directive in mf for the module path at version.
func userReplacement(mf *modfile.File, path, version string) (string, bool) {
	var found *modfile.Replace
	for _, r := range mf.Replace {
		if r.Old.Path != path {
			continue
		}
		if r.Old.Version == version {
			found = r
			break
		}
		if r.Old.Version == "" {
			found = r
		}
	}
	if found == nil {
		return "", false
	}
	if found.New.Version == "" {
		return found.New.Path, true
	}
	return found.New.Path + " " + found.New.Version, true
}

// relativeReplacePath returns the path of target relative to moduleDir, in
// the form that go.mod needs for a directory replacement ("./x" or "../x").
// A relative path stays correct when the project moves to a different
// directory or machine. If target has no relative path, it is returned as is.
func relativeReplacePath(moduleDir, target string) string {
	rel, err := filepath.Rel(moduleDir, target)
	if err != nil {
		return target
	}
	rel = filepath.ToSlash(rel)
	if rel != ".." && !strings.HasPrefix(rel, "../") {
		rel = "./" + rel
	}
	return rel
}

type vendorCopy struct {
	src string
	dst string
}

// vendorCopyRoots are the directories of planVendorCopies.
type vendorCopyRoots struct {
	// scratchVendor is the vendor directory of the scratch module.
	scratchVendor string
	// moduleDir is the module directory of the user. Its vendor directory
	// gets the copies.
	moduleDir string
	// deleted are the files of vendor/ that are removed before the copy. A
	// deleted file can be copied again.
	deleted map[string]bool
}

// planVendorCopies returns the files to copy from the vendor directory of the
// scratch module to the vendor directory of the user, and the directories to
// make (parents first). It copies the files of each added package (with the
// embedded files in subdirectories, see packageFiles), and the files (such as
// LICENSE) of the parent directories up to the module root that are not
// packages. A file that already exists (and is not deleted) is never
// replaced. A symbolic link in a destination path is an error.
func planVendorCopies(merged *modulesTxt, added []string, roots vendorCopyRoots) ([]vendorCopy, []string, error) {
	isPackage := vendoredPackages(merged)
	userVendor := filepath.Join(roots.moduleDir, vendorDirName)
	plan := &copyPlan{
		roots:      roots,
		userVendor: userVendor,
		own:        vendoredDirsOf(merged),
		seen:       make(map[string]bool),
	}
	for _, pkg := range added {
		if err := plan.addPackage(pkg); err != nil {
			return nil, nil, err
		}
		mod := merged.moduleOfPackage(pkg)
		if mod == nil {
			continue
		}
		for _, parent := range parentDirs(pkg, mod.Path) {
			if isPackage[parent] {
				continue
			}
			if err := plan.addDir(parent); err != nil {
				return nil, nil, err
			}
		}
	}

	slices.SortFunc(plan.copies, func(a, b vendorCopy) int { return strings.Compare(a.dst, b.dst) })
	return plan.copies, missingDirs(plan.copies, userVendor), nil
}

// parentDirs returns the parent import paths of pkg, up to and with the module
// root modPath.
func parentDirs(pkg, modPath string) []string {
	var parents []string
	for p := pkg; p != modPath && strings.HasPrefix(p, modPath+"/"); {
		p = p[:strings.LastIndex(p, "/")]
		parents = append(parents, p)
	}
	return parents
}

// missingDirs returns the directories below userVendor that the copies need
// and that do not exist yet, sorted (parents first).
func missingDirs(copies []vendorCopy, userVendor string) []string {
	dirSet := make(map[string]bool)
	for _, c := range copies {
		for d := filepath.Dir(c.dst); d != userVendor && strings.HasPrefix(d, userVendor); d = filepath.Dir(d) {
			if lexists(d) {
				break
			}
			dirSet[d] = true
		}
	}
	return slices.Sorted(maps.Keys(dirSet))
}

// copyPlan collects the files that planVendorCopies copies.
type copyPlan struct {
	roots      vendorCopyRoots
	userVendor string
	// own are the packages and module roots of the merged modules.txt.
	own    vendoredDirs
	seen   map[string]bool
	copies []vendorCopy
}

// addPackage adds the files of the package pkg (see packageFiles) that the
// vendor directory of the user does not have.
func (p *copyPlan) addPackage(pkg string) error {
	srcDir := filepath.Join(p.roots.scratchVendor, filepath.FromSlash(pkg))
	files, err := packageFiles(p.roots.scratchVendor, srcDir, pkg, p.own)
	if err != nil {
		return err
	}
	return p.add(files)
}

// addDir adds the regular files of the directory importDir (an import path
// below vendor/, not its subdirectories) that the vendor directory of the
// user does not have.
func (p *copyPlan) addDir(importDir string) error {
	srcDir := filepath.Join(p.roots.scratchVendor, filepath.FromSlash(importDir))
	files, err := regularFiles(p.roots.scratchVendor, srcDir)
	if err != nil {
		return err
	}
	return p.add(files)
}

// add adds the files (in the vendor directory of the scratch module) that
// the vendor directory of the user does not have.
func (p *copyPlan) add(files []string) error {
	for _, src := range files {
		rel, err := filepath.Rel(p.roots.scratchVendor, src)
		if err != nil {
			return ex.Wrap(err)
		}
		dst := filepath.Join(p.userVendor, rel)
		if err = checkNoSymlink(p.roots.moduleDir, dst); err != nil {
			return err
		}
		if p.seen[dst] || (lexists(dst) && !p.roots.deleted[dst]) {
			continue
		}
		p.seen[dst] = true
		p.copies = append(p.copies, vendorCopy{src: src, dst: dst})
	}
	return nil
}

// fillVendoredVersions sets the version of each dependency that has its
// sources in a vendor directory. The path of a vendored file has no
// "@version" part, so the version comes from vendor/modules.txt. Rules with a
// version range need it.
func fillVendoredVersions(ctx context.Context, deps []*Dependency) {
	logger := util.LoggerFromContext(ctx)
	cache := make(map[string]*modulesTxt)
	for _, dep := range deps {
		if dep.Version != "" || len(dep.Sources) == 0 || dep.ImportPath == "" {
			continue
		}
		src := filepath.ToSlash(dep.Sources[0])
		idx := strings.LastIndex(src, "/"+vendorDirName+"/"+dep.ImportPath+"/")
		if idx < 0 {
			continue
		}
		root := filepath.FromSlash(src[:idx])
		m, seen := cache[root]
		if !seen {
			path := filepath.Join(root, vendorDirName, modulesTxtFileName)
			data, err := os.ReadFile(path)
			if err == nil {
				m, err = parseModulesTxt(data)
			}
			if err != nil {
				logger.DebugContext(ctx, "cannot read vendor/modules.txt", "path", path, "error", err)
				m = nil
			}
			cache[root] = m
		}
		if m == nil {
			continue
		}
		if mod := m.moduleOfPackage(dep.ImportPath); mod != nil {
			dep.Version = mod.Version
		}
	}
}
