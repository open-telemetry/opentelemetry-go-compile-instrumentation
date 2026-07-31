// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/util"
)

// vendoringActive reports whether the main module vendors its dependencies. It
// resolves the module via a single go env GOMOD/GOWORK call rather than
// `go list`, which would fail the vendor consistency check while go.mod is
// mid-edit, then checks for vendor/modules.txt. Returns false in workspace
// mode: Go forbids -mod=mod there, so forcing it would turn a build that
// worked into a hard failure.
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
	return util.PathExists(filepath.Join(root, "vendor", "modules.txt"))
}

// modMod forces module mode, so the build ignores the vendor directory.
const modMod = "-mod=mod"

// isModFlag reports whether tok is a bare -mod/--mod flag; Go's flag parser
// treats the double-dash form the same as the single-dash one. The value, if
// any, is the next argument.
func isModFlag(tok string) bool {
	return tok == "-mod" || tok == "--mod"
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
	fields := strings.Fields(goflags)
	hasMod := false
	for i, f := range fields {
		switch {
		case isModVendorToken(f):
			fields[i] = modMod
			hasMod = true
		case isModToken(f):
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
func rewriteModVendor(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i < len(out); i++ {
		switch {
		case isModVendorToken(out[i]):
			out[i] = modMod
		case isModFlag(out[i]) && i+1 < len(out) && out[i+1] == "vendor":
			out[i+1] = "mod"
			i++
		}
	}
	return out
}
