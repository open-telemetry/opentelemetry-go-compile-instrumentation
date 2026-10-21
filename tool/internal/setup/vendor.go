// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	goversion "go/version"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// minVendorGoVersion is the lowest go version in go.mod for which the go
// command uses the vendor directory by default. For a lower version (or no go
// directive), the go command ignores vendor/. otelc supports vendor mode only
// from this version.
const minVendorGoVersion = "1.14"

// vendoredModuleRoot returns the directory of the main module when it vendors
// its dependencies, else "". It resolves the module via a single go env
// GOMOD/GOWORK call rather than `go list`, which would fail the vendor
// consistency check while go.mod is mid-edit, then checks for
// vendor/modules.txt. Returns "" in workspace mode: there the go command uses
// the vendor directory of the workspace, not the one of the module.
func vendoredModuleRoot(ctx context.Context, workDir string) string {
	logger := util.LoggerFromContext(ctx)

	root, workspace, err := pkgload.ModuleAndWorkspace(ctx, workDir)
	if err != nil {
		logger.WarnContext(ctx, "failed to resolve module/workspace state; building as non-vendored",
			"dir", workDir, "error", err)
		return ""
	}
	if workspace || root == "" {
		return ""
	}
	if !util.PathExists(filepath.Join(root, vendorDirName, modulesTxtFileName)) {
		return ""
	}
	return root
}

// vendorGoVersionSupported reports whether the go directive of go.mod in
// moduleRoot is minVendorGoVersion or higher. A go.mod without a go directive
// gives false: the go command then ignores vendor/ too.
func vendorGoVersionSupported(moduleRoot string) (bool, error) {
	f, err := parseGoMod(filepath.Join(moduleRoot, goModFileName))
	if err != nil {
		return false, err
	}
	if f.Go == nil {
		return false, nil
	}
	return goversion.Compare("go"+f.Go.Version, "go"+minVendorGoVersion) >= 0, nil
}

// requireVendorGoVersion returns an error when the go directive of go.mod in
// moduleRoot is lower than minVendorGoVersion. otelc does not support vendor
// mode for such a module.
func requireVendorGoVersion(moduleRoot string) error {
	ok, err := vendorGoVersionSupported(moduleRoot)
	if err != nil || ok {
		return err
	}
	return ex.Newf("otelc needs go %s or higher in %s for vendor mode; run \"go mod edit -go=%s && go mod vendor\"",
		minVendorGoVersion, filepath.Join(moduleRoot, goModFileName), minVendorGoVersion)
}

// buildMode is the module mode of a build.
type buildMode struct {
	// vendored is true when the build uses the vendor directory.
	vendored bool
	// modFlag is the value of the last -mod flag of the build (in GOFLAGS or
	// in the arguments), or "" when no -mod flag is set.
	modFlag string
}

// buildModeKey is the context key of the module mode of the build.
type buildModeKey struct{}

// contextWithVendorMode returns a copy of ctx that records whether the build
// uses the vendor directory. The build has no -mod flag.
func contextWithVendorMode(ctx context.Context, vendored bool) context.Context {
	return context.WithValue(ctx, buildModeKey{}, buildMode{vendored: vendored})
}

// contextWithBuildMode returns a copy of ctx that records the module mode of
// a build with the given arguments: the vendor mode (see vendorMode) and the
// -mod flag. The directory of the build is the -C directory of args, else the
// otelc work directory. If ctx has a module mode already, contextWithBuildMode
// returns ctx: the mode is found one time for each otelc command.
func contextWithBuildMode(ctx context.Context, subcommand string, args []string) (context.Context, error) {
	if _, ok := ctx.Value(buildModeKey{}).(buildMode); ok {
		return ctx, nil
	}
	dir, err := buildWorkDir(subcommand, args)
	if err != nil {
		return nil, err
	}
	vendored, err := vendorMode(ctx, dir, subcommand, args)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, buildModeKey{}, buildMode{
		vendored: vendored,
		modFlag:  lastModFlag(subcommand, args),
	}), nil
}

// vendorModeFromContext returns the vendor mode that contextWithVendorMode or
// contextWithBuildMode recorded. The second result is false when ctx has no
// vendor mode.
//
//nolint:revive // needed to balance confusing-results and nonamedreturns linters
func vendorModeFromContext(ctx context.Context) (bool, bool) {
	mode, ok := ctx.Value(buildModeKey{}).(buildMode)
	return mode.vendored, ok
}

// modBuildFlags returns the -mod flag of the build that ctx records, as build
// flags for a package load, or nil when the build has no -mod flag. The loads
// that otelc does after it changed go.mod must use the same mode as the
// build: else, for example, a -mod=mod build of a vendored module loads in
// vendor mode, and the go command reports "inconsistent vendoring". A -mod
// flag in GOFLAGS also goes to the load through the environment; to give it
// again is harmless.
func modBuildFlags(ctx context.Context) []string {
	mode, ok := ctx.Value(buildModeKey{}).(buildMode)
	if !ok || mode.modFlag == "" {
		return nil
	}
	return []string{flagMod + "=" + mode.modFlag}
}

// buildWorkDir returns the directory of a build with the given arguments: the
// -C directory of args, else the otelc work directory. Package loading
// (pkgload.LoadPackages) resolves -C in the same way.
func buildWorkDir(subcommand string, args []string) (string, error) {
	if subcommand == "" {
		subcommand = subcmdBuild
	}
	dir, err := pkgload.BuildDir(extractBuildFlags(subcommand, args))
	if err != nil {
		return "", err
	}
	if dir == "" {
		return util.GetOtelcWorkDir(), nil
	}
	return dir, nil
}

// vendorMode reports whether the go command uses the vendor directory for a
// build in workDir with the given arguments. This is the case when:
//   - the module vendors its dependencies (see vendoredModuleRoot), and
//   - no -mod=mod or -mod=readonly flag (in GOFLAGS or in args) makes the go
//     command ignore vendor/, and
//   - a -mod=vendor flag is set, or the go directive of go.mod is
//     minVendorGoVersion or higher (else the go command ignores vendor/).
//
// A -mod=vendor flag with a go directive lower than minVendorGoVersion is an
// error: otelc does not support vendor mode for such a module.
func vendorMode(ctx context.Context, workDir, subcommand string, args []string) (bool, error) {
	if v, ok := vendorModeFromContext(ctx); ok {
		return v, nil
	}
	root := vendoredModuleRoot(ctx, workDir)
	if root == "" {
		return false, nil
	}
	switch lastModFlag(subcommand, args) {
	case "mod", "readonly":
		return false, nil
	case vendorDirName:
		if err := requireVendorGoVersion(root); err != nil {
			return false, err
		}
		return true, nil
	default:
		return vendorGoVersionSupported(root)
	}
}

// vendorSyncHook is vendorSync. It is a variable so tests can see the imports
// that reach vendorSync.
//
//nolint:gochecknoglobals // test seam
var vendorSyncHook = vendorSync

// ruleSetImports returns the packages that the rules of matched add to the
// build: the hook packages of the function and file rules, and the values of
// the imports maps of all the rules.
func ruleSetImports(matched []*rule.InstRuleSet) map[string]bool {
	imports := make(map[string]bool)
	for _, path := range hookPackagePaths(matched) {
		imports[path] = true
	}
	for _, rs := range matched {
		for _, base := range baseRules(rs) {
			for _, path := range base.Imports {
				if path != "" {
					imports[path] = true
				}
			}
		}
	}
	return imports
}

// baseRules returns the base rules of all the rules of rs.
func baseRules(rs *rule.InstRuleSet) []*rule.InstBaseRule {
	var bases []*rule.InstBaseRule
	bases = appendBaseRules(bases, rs.RawRules, func(r *rule.InstRawRule) *rule.InstBaseRule { return &r.InstBaseRule })
	bases = appendBaseRules(
		bases,
		rs.FuncRules,
		func(r *rule.InstFuncRule) *rule.InstBaseRule { return &r.InstBaseRule },
	)
	bases = appendBaseRules(bases, rs.StructRules,
		func(r *rule.InstStructRule) *rule.InstBaseRule { return &r.InstBaseRule })
	bases = appendBaseRules(
		bases,
		rs.CallRules,
		func(r *rule.InstCallRule) *rule.InstBaseRule { return &r.InstBaseRule },
	)
	bases = appendBaseRules(bases, rs.LitRules, func(r *rule.InstLitRule) *rule.InstBaseRule { return &r.InstBaseRule })
	bases = appendBaseRules(bases, rs.DirectiveRules,
		func(r *rule.InstDirectiveRule) *rule.InstBaseRule { return &r.InstBaseRule })
	bases = appendBaseRules(
		bases,
		rs.DeclRules,
		func(r *rule.InstDeclRule) *rule.InstBaseRule { return &r.InstBaseRule },
	)
	for _, r := range rs.FileRules {
		bases = append(bases, &r.InstBaseRule)
	}
	return bases
}

// appendBaseRules appends to bases the base rule of each rule in byFile.
func appendBaseRules[R any](
	bases []*rule.InstBaseRule,
	byFile map[string][]R,
	base func(R) *rule.InstBaseRule,
) []*rule.InstBaseRule {
	for _, rules := range byFile {
		for _, r := range rules {
			bases = append(bases, base(r))
		}
	}
	return bases
}

// vendorSyncRuleSets adds to the vendor directory of each module in
// moduleDirs the packages that the rules of matched add to the build (see
// ruleSetImports). It is for custom rules (--rules or OTELC_RULES): their hook
// packages are only required in go.mod, and not in vendor/. Before the
// change, it tracks go.mod, go.sum and the tool file of each module, so the
// state manager restores them after the build. vendorSync tracks the files in
// vendor/.
func vendorSyncRuleSets(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs []string) error {
	imports := ruleSetImports(matched)
	if len(imports) == 0 {
		return nil
	}
	stateManager, found := stateManagerFromContext(ctx)
	if !found {
		return ex.New("state manager not found in context")
	}
	backupFiles, err := getBackupFiles(ctx, moduleDirs)
	if err != nil {
		return err
	}
	if err = stateManager.TrackAll(backupFiles...); err != nil {
		return err
	}
	for _, moduleDir := range normalizeModuleDirs(moduleDirs) {
		if err = vendorSyncHook(ctx, imports, moduleDir); err != nil {
			return err
		}
	}
	return nil
}

// isModFlag reports whether tok is a bare -mod/--mod flag; Go's flag parser
// treats the double-dash form the same as the single-dash one. The value, if
// any, is the next argument.
func isModFlag(tok string) bool {
	return tok == flagMod || tok == "--mod"
}

// modFlagValue returns the value of a joined -mod=value/--mod=value token.
func modFlagValue(tok string) (string, bool) {
	if v, ok := strings.CutPrefix(tok, "-mod="); ok {
		return v, true
	}
	return strings.CutPrefix(tok, "--mod=")
}

// lastModFlag returns the value of the last -mod flag. A flag in args wins
// over GOFLAGS. It returns "" when no -mod flag is set.
func lastModFlag(subcommand string, args []string) string {
	if subcommand == "" {
		subcommand = subcmdBuild
	}
	value := ""
	tokens := util.SplitGoflags(os.Getenv("GOFLAGS"))
	for _, tok := range tokens {
		if v, ok := modFlagValue(util.UnquoteGoflagsToken(tok)); ok {
			value = v
		}
	}
	classified := classifyArgs(subcommand, args)
	for i, a := range classified {
		if a.Kind != ArgBuildFlag || a.FlagName != flagMod {
			continue
		}
		if a.HasValue {
			value = a.Value
		} else if i+1 < len(classified) && classified[i+1].Kind == ArgBuildFlagValue {
			value = classified[i+1].Raw
		}
	}
	return value
}

// goflagsWithoutMod returns goflags without its -mod tokens. The go commands
// that otelc runs in its scratch module use module mode, and a -mod=vendor of
// the user must not change this.
func goflagsWithoutMod(goflags string) string {
	tokens := util.SplitGoflags(goflags)
	kept := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		unquoted := util.UnquoteGoflagsToken(tok)
		if _, ok := modFlagValue(unquoted); ok || isModFlag(unquoted) {
			continue
		}
		kept = append(kept, tok)
	}
	return strings.Join(kept, " ")
}

// envWithModFlag returns env with a GOFLAGS variable that has -mod=mode in
// place of the -mod tokens of the GOFLAGS of the process. It returns env when
// mode is "". exec.Cmd uses the last value of a variable, so this one wins.
func envWithModFlag(env []string, mode string) []string {
	if mode == "" {
		return env
	}
	goflags := strings.TrimSpace(goflagsWithoutMod(os.Getenv("GOFLAGS")) + " " + flagMod + "=" + mode)
	return append(env, "GOFLAGS="+goflags)
}

// goflagsWithoutModfile returns goflags without its -modfile tokens.
func goflagsWithoutModfile(goflags string) string {
	tokens := util.SplitGoflags(goflags)
	kept := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if isModfileFlag(util.UnquoteGoflagsToken(tok)) {
			continue
		}
		kept = append(kept, tok)
	}
	return strings.Join(kept, " ")
}

// flagModfile is the -modfile flag of the go command.
const flagModfile = "-modfile"

// isModfileFlag reports whether tok is a -modfile flag: -modfile,
// -modfile=value, or the same with two dashes.
func isModfileFlag(tok string) bool {
	// Go's flag parser accepts one or two dashes.
	if strings.HasPrefix(tok, "--") {
		tok = tok[1:]
	}
	return tok == flagModfile || strings.HasPrefix(tok, flagModfile+"=")
}
