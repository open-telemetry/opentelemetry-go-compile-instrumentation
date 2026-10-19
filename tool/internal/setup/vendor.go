// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/util"
)

// vendoringActive reports whether the main module vendors its dependencies. It
// resolves the module via a single go env GOMOD/GOWORK call rather than
// `go list`, which would fail the vendor consistency check while go.mod is
// mid-edit, then checks for vendor/modules.txt. Returns false in workspace
// mode: there the go command uses the vendor directory of the workspace, not
// the one of the module.
func vendoringActive(ctx context.Context, workDir string) bool {
	logger := util.LoggerFromContext(ctx)

	root, workspace, err := pkgload.ModuleAndWorkspace(ctx, workDir)
	if err != nil {
		logger.WarnContext(ctx, "failed to resolve module/workspace state; building as non-vendored",
			"dir", workDir, "error", err)
		return false
	}
	if workspace {
		return false
	}
	if root == "" {
		return false
	}
	return util.PathExists(filepath.Join(root, vendorDirName, "modules.txt"))
}

// vendorModeKey is the context key of the vendor mode of the build.
type vendorModeKey struct{}

// contextWithVendorMode returns a copy of ctx that records whether the build
// uses the vendor directory.
func contextWithVendorMode(ctx context.Context, vendored bool) context.Context {
	return context.WithValue(ctx, vendorModeKey{}, vendored)
}

// vendorModeFromContext returns the vendor mode that contextWithVendorMode
// recorded. The second result is false when ctx has no vendor mode.
//
//nolint:revive // needed to balance confusing-results and nonamedreturns linters
func vendorModeFromContext(ctx context.Context) (bool, bool) {
	vendored, ok := ctx.Value(vendorModeKey{}).(bool)
	return vendored, ok
}

// vendorMode reports whether the go command uses the vendor directory for a
// build in workDir with the given arguments. This is the case when the
// module vendors its dependencies (see vendoringActive), and no -mod=mod or
// -mod=readonly flag (in GOFLAGS or in args) makes the go command ignore
// vendor/.
func vendorMode(ctx context.Context, workDir, subcommand string, args []string) bool {
	if v, ok := vendorModeFromContext(ctx); ok {
		return v
	}
	if !vendoringActive(ctx, workDir) {
		return false
	}
	switch lastModFlag(subcommand, args) {
	case "mod", "readonly":
		return false
	default:
		return true
	}
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
