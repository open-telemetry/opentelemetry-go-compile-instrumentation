// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/util"
)

// hasExportTool reports whether the toolchain of compileTool ships cmd/export
// (Go 1.28+). With it, go list -export writes tools-format export data
// instead of pointing at compiler archives (golang/go#79427).
func hasExportTool(compileTool string) bool {
	// The tools share an extension: .exe on Windows, none elsewhere.
	_, err := os.Stat(filepath.Join(filepath.Dir(compileTool), "export"+filepath.Ext(compileTool)))
	return err == nil
}

// exportDataFiles returns the export data file of each import of the
// package being compiled, for type-checking it. Before Go 1.28 these are the
// compiler archives in -importcfg. From Go 1.28 the compiler writes a format
// tools cannot read, so the files come from go list -export: cmd/export
// type-checks the original source, which is also what type-checking here
// reads.
//
// shortcut: go list returns the plain variant of each import, so in a go test
// build symbols only a package's export_test.go declares do not resolve and
// calls through them do not match; list with -test if that matters.
func (ip *instrumentPhase) exportDataFiles(ctx context.Context) (map[string]string, error) {
	if len(ip.compileArgs) == 0 || !hasExportTool(ip.compileArgs[0]) {
		return ip.importConfig.PackageFile, nil
	}

	paths := make([]string, 0, len(ip.importConfig.PackageFile))
	for path := range ip.importConfig.PackageFile {
		if path != unsafePackageName && path != "C" {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)

	resolve := ip.resolveExportFiles
	if resolve == nil {
		resolve = pkgload.ResolveExportFilesBatch
	}
	files, failed, err := resolve(ctx, paths, util.GetBuildFlags()...)
	if err != nil {
		return nil, ex.Wrapf(err, "resolving export data with go list -export")
	}
	for path, loadErr := range failed {
		// Calls through this import stay unresolved; the rest still match.
		ip.Debug("method_call: no export data for import", "import", path, "error", loadErr)
	}
	ip.Debug("method_call: using go list -export for type info",
		"package", util.FindFlagValue(ip.compileArgs, "-p"), "files", len(files))
	return files, nil
}
