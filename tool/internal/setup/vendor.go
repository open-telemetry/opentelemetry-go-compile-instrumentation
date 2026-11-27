// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bufio"
	"context"
	"go/parser"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/util"
)

const modulesTxtFileName = "modules.txt"

// unversionedLocalReplaceVersion is the pseudo-version Go writes to
// vendor/modules.txt when go.mod replaces a module with a local path without
// specifying a version on the left-hand side of "=>".
const unversionedLocalReplaceVersion = "v0.0.0-00010101000000-000000000000"

type vendorModules map[string]string

type vendorState struct {
	// vendored is true when a single-module vendor/modules.txt is active and
	// all external tool-file imports are vendored, so -mod=vendor stays enabled.
	vendored   bool
	queryDir   string
	moduleRoot string
	modules    vendorModules
}

type vendorStateKey struct{}

func resolveAbsDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	return resolveCanonicalPath(abs)
}

func resolveCanonicalPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	} else {
		p = filepath.Clean(p)
	}
	var suffix []string
	for cur := p; ; {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			for _, elem := range slices.Backward(suffix) {
				resolved = filepath.Join(resolved, elem)
			}
			return resolved
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

func contextWithVendorState(ctx context.Context, vs vendorState) context.Context {
	return context.WithValue(ctx, vendorStateKey{}, vs)
}

func vendorStateFromContext(ctx context.Context, dir string) (vendorState, bool) {
	vs, ok := ctx.Value(vendorStateKey{}).(vendorState)
	if !ok {
		return vendorState{}, false
	}
	absDir := resolveAbsDir(dir)
	if absDir == "" {
		return vendorState{}, false
	}
	if absDir == vs.queryDir || (vs.moduleRoot != "" && absDir == vs.moduleRoot) {
		return vs, true
	}
	return vendorState{}, false
}

// getVendorState returns the vendorState cached on ctx for dir, or computes it.
func getVendorState(ctx context.Context, dir string) vendorState {
	if vs, ok := vendorStateFromContext(ctx, dir); ok {
		return vs
	}
	return detectVendorState(ctx, dir)
}

func (vs vendorState) active() bool {
	return vs.moduleRoot != ""
}

func (vs vendorState) needsModMod() bool {
	return vs.moduleRoot != "" && !vs.vendored
}

func (vs vendorState) vendorDir() string {
	if vs.moduleRoot == "" {
		return ""
	}
	return filepath.Join(vs.moduleRoot, vendorDirName)
}

func (vs vendorState) packageVersion(importPath, sourceFile string) string {
	if len(vs.modules) == 0 || sourceFile == "" || !isUnderDir(sourceFile, vs.vendorDir()) {
		return ""
	}
	return vs.modules[importPath]
}

// parseVendorModules parses a Go vendor/modules.txt manifest and maps every
// listed package import path to its resolved module version (or "" for
// unversioned local replacements).
func parseVendorModules(path string) (vendorModules, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	vm := make(vendorModules)
	currentVersion := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "##") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "# "); ok {
			currentVersion = parseVendorModuleHeaderVersion(rest)
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		vm[line] = currentVersion
	}
	if err = scanner.Err(); err != nil {
		return nil, err
	}
	return vm, nil
}

// parseVendorModuleHeaderVersion extracts the effective semantic version from
// the body of a "# <module>[ <version>][ => <replacement>]" header line in
// vendor/modules.txt.
func parseVendorModuleHeaderVersion(header string) string {
	const minVersionFields = 2
	lhs, rhs, hasReplace := strings.Cut(header, " => ")
	lhsVersion := ""
	if lhsFields := strings.Fields(lhs); len(lhsFields) >= minVersionFields && semver.IsValid(lhsFields[1]) {
		if lhsFields[1] != unversionedLocalReplaceVersion {
			lhsVersion = lhsFields[1]
		}
	}
	if !hasReplace {
		return lhsVersion
	}
	// Versioned module replacement: "# mod v1.0.0 => fork v1.2.0"
	if rhsFields := strings.Fields(rhs); len(rhsFields) >= minVersionFields && semver.IsValid(rhsFields[1]) {
		return rhsFields[1]
	}
	// Local path replacement: "# mod v1.0.0 => ../local"
	return lhsVersion
}

// isUnderDir reports whether path resides inside dir.
func isUnderDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(resolveCanonicalPath(dir), resolveCanonicalPath(path))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveVendorModuleRoot resolves the single-module root for dir when
// vendor/modules.txt is present. Returns "" outside a module, without
// vendor/modules.txt, or inside a go.work workspace (Go forbids -mod=mod there).
func resolveVendorModuleRoot(ctx context.Context, dir string) string {
	logger := util.LoggerFromContext(ctx)

	root, workspace, err := pkgload.ModuleAndWorkspace(ctx, dir)
	if err != nil {
		logger.WarnContext(ctx, "failed to resolve module/workspace state; building as non-vendored",
			"dir", dir, "error", err)
		return ""
	}
	if workspace || root == "" {
		return ""
	}
	if !util.PathExists(filepath.Join(root, vendorDirName, modulesTxtFileName)) {
		return ""
	}
	return filepath.Clean(root)
}

// detectVendorState inspects dir to determine whether single-module vendoring
// is active and whether all external tool-file imports are already vendored.
func detectVendorState(ctx context.Context, dir string) vendorState {
	queryDir := resolveAbsDir(dir)
	root := resolveVendorModuleRoot(ctx, dir)
	if root == "" {
		return vendorState{queryDir: queryDir}
	}

	modulesTxt := filepath.Join(root, vendorDirName, modulesTxtFileName)
	vm, err := parseVendorModules(modulesTxt)
	if err != nil {
		util.LoggerFromContext(ctx).WarnContext(ctx, "failed to parse vendor/modules.txt",
			"path", modulesTxt, "error", err)
		return vendorState{queryDir: queryDir, moduleRoot: root}
	}

	toolFile, err := findToolFile(root)
	vendored := err == nil && toolFile != "" && toolFileVendored(root, toolFile, vm)
	return vendorState{
		vendored:   vendored,
		queryDir:   queryDir,
		moduleRoot: root,
		modules:    vm,
	}
}

// toolFileVendored reports whether every external import declared in toolFile
// is listed in vm. Packages belonging to root's own module are satisfied
// without appearing in vendor/modules.txt.
func toolFileVendored(root, toolFile string, vm vendorModules) bool {
	f, parseErr := ast.NewAstParser().Parse(toolFile, parser.ImportsOnly)
	if parseErr != nil {
		return false
	}
	importPaths, collectErr := collectImports(toolFile, f, make(map[string]bool))
	if collectErr != nil {
		return false
	}

	var mainModPath string
	if mf, modErr := parseGoMod(filepath.Join(root, goModFileName)); modErr == nil && mf.Module != nil {
		mainModPath = mf.Module.Mod.Path
	}

	for _, imp := range importPaths {
		if _, ok := vm[imp]; ok {
			continue
		}
		if isLocalModulePackage(root, mainModPath, imp) {
			continue
		}
		return false
	}
	return true
}

func isLocalModulePackage(root, mainModPath, importPath string) bool {
	if mainModPath == "" {
		return false
	}
	if importPath == mainModPath {
		return true
	}
	rel, ok := strings.CutPrefix(importPath, mainModPath+"/")
	if !ok {
		return false
	}
	pkgDir := filepath.Join(root, filepath.FromSlash(rel))
	if !util.PathExists(pkgDir) {
		return false
	}
	// If pkgDir or any ancestor below root has its own go.mod, the package
	// belongs to a nested submodule (not root's module) and must be vendored.
	cleanRoot := filepath.Clean(root)
	for cur := filepath.Clean(pkgDir); cur != cleanRoot && cur != filepath.Dir(cur); cur = filepath.Dir(cur) {
		if util.PathExists(filepath.Join(cur, goModFileName)) {
			return false
		}
	}
	return true
}

// buildDir returns the directory that the go command runs a build with args
// in: the -C directory when args set one, or else "", which runs a command in
// the current directory. A relative -C directory resolves against the current
// directory, for the go command and for exec.Cmd.Dir alike. The otelc work
// directory is not that directory, because --work-dir can point anywhere.
func buildDir(subcommand string, args []string) string {
	dir := ""
	for _, a := range classifyArgs(subcommand, args) {
		if a.FlagName == "-C" && (a.Kind == ArgBuildFlag || a.Kind == ArgBuildFlagValue) && a.Value != "" {
			dir = a.Value
		}
	}
	return dir
}

// vendoringActive reports whether the main module of a build in dir vendors
// its dependencies. dir is the directory the go command builds in, see
// buildDir. Returns false in workspace mode: Go forbids -mod=mod there.
func vendoringActive(ctx context.Context, dir string) bool {
	if vs, ok := vendorStateFromContext(ctx, dir); ok {
		return vs.active()
	}
	return resolveVendorModuleRoot(ctx, dir) != ""
}

func forceModuleMode(ctx context.Context, subcommand string, args []string) ([]string, error) {
	util.LoggerFromContext(ctx).InfoContext(ctx, "vendored project detected; building with -mod=mod")

	if err := os.Setenv("GOFLAGS", forceModMod(os.Getenv("GOFLAGS"))); err != nil {
		return nil, ex.Wrapf(err, "forcing module mode for vendored build")
	}

	return rewriteModVendor(subcommand, args), nil
}

// modMod forces module mode, so the build ignores the vendor directory.
const modMod = "-mod=mod"

// isModFlag reports whether tok is a bare -mod/--mod flag; Go's flag parser
// treats the double-dash form the same as the single-dash one. The value, if
// any, is the next argument.
func isModFlag(tok string) bool {
	return tok == flagMod || tok == "--mod"
}

// isModVendorToken reports whether tok is a joined -mod=vendor/--mod=vendor
// flag.
func isModVendorToken(tok string) bool {
	return tok == "-mod=vendor" || tok == "--mod=vendor"
}

// isModToken reports whether tok is any form of the -mod/--mod flag: bare
// (-mod, --mod) or joined with a value (-mod=value, --mod=value).
func isModToken(tok string) bool {
	return isModFlag(tok) || strings.HasPrefix(tok, "-mod=") || strings.HasPrefix(tok, "--mod=")
}

// forceModMod returns goflags adjusted so the build ignores the vendor
// directory. GOFLAGS holds space-separated single tokens, so only the
// -mod=value form (single- or double-dash: -mod=vendor, --mod=vendor) occurs
// here. Every -mod=vendor/--mod=vendor is rewritten to -mod=mod (Go applies
// last-wins for repeated flags, so all occurrences must change); -mod=mod and
// -mod=readonly (either dash form) are left as-is (both already ignore
// vendoring, so we respect the user's intent). -mod=mod is appended only when
// no -mod/--mod token is present at all.
func forceModMod(goflags string) string {
	// SplitGoflags keeps quoted tokens (such as -tags='foo bar') intact, unlike
	// strings.Fields which would split them on the inner space.
	fields := util.SplitGoflags(goflags)
	hasMod := false
	for i, f := range fields {
		// The go command strips one layer of surrounding quotes from a GOFLAGS
		// token before interpreting it, so a quoted flag such as '-mod=readonly'
		// is a -mod flag. Match on the unquoted form, but keep the original
		// token in the output so an existing -mod choice is preserved verbatim.
		switch tok := util.UnquoteGoflagsToken(f); {
		case isModVendorToken(tok):
			fields[i] = modMod
			hasMod = true
		case isModToken(tok):
			hasMod = true
		}
	}
	if !hasMod {
		fields = append(fields, modMod)
	}
	return strings.Join(fields, " ")
}

// rewriteModVendor returns a copy of args with an explicit -mod=vendor/
// --mod=vendor (single token) or -mod vendor/--mod vendor (two tokens)
// rewritten to module mode, leaving -mod=readonly and -mod=mod untouched. A
// CLI flag beats GOFLAGS, so this neutralizes a vendor selection that setting
// GOFLAGS alone cannot override.
// Only actual Go build flags are rewritten; test-binary arguments and test flag
// values are preserved untouched.
func rewriteModVendor(subcommand string, args []string) []string {
	if subcommand == "" {
		subcommand = subcmdBuild
	}
	classified := classifyArgs(subcommand, args)
	out := make([]string, len(args))
	copy(out, args)

	for i, a := range classified {
		if a.Kind == ArgBuildFlag && a.FlagName == flagMod {
			if a.HasValue && isModVendorToken(a.Raw) {
				out[a.Index] = modMod
			} else if !a.HasValue && i+1 < len(classified) && classified[i+1].Kind == ArgBuildFlagValue && classified[i+1].Raw == vendorDirName {
				out[classified[i+1].Index] = "mod"
			}
		}
	}
	return out
}
