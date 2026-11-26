// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"os"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/buildserver"
	"go.opentelemetry.io/otelc/tool/util"
)

// loadCompileStateFromServer queries the in-memory build server for pkgPath.
// A nil response means the server was unavailable and the caller should fall
// back to disk.
func (ip *instrumentPhase) loadCompileStateFromServer(
	ctx context.Context,
	pkgPath string,
) *buildserver.CompileResponse {
	if pkgPath == "" {
		return nil
	}
	client := buildserver.ClientFromEnv()
	if client == nil {
		return nil
	}
	resp, err := client.CompileLookup(ctx, pkgPath)
	if err != nil {
		return nil
	}
	if !resp.Matched {
		return resp
	}
	ip.buildUsesIgnoreDirective = resp.BuildUsesIgnoreDirective
	if resp.ImportNames != nil {
		ip.importNames = resp.ImportNames
	} else {
		ip.importNames = map[string]string{}
	}
	ip.pgoProfile = resp.PGOProfile
	ip.pgoProfileLoaded = true
	return resp
}

// resolveImportsViaServer attempts to resolve missing imports through the
// in-memory build server. Returning buildserver.ErrUnavailable signals the
// caller to fall back to the local memo + go list path.
func (ip *instrumentPhase) resolveImportsViaServer(
	ctx context.Context,
	missing []string,
	usePGO bool,
	memoHits, goListCalls *int,
) (bool, error) {
	if ip.resolveAddedImport != nil {
		return false, buildserver.ErrUnavailable
	}
	client := buildserver.ClientFromEnv()
	if client == nil {
		return false, buildserver.ErrUnavailable
	}
	chain, err := ip.validateResolutionChain(missing)
	if err != nil {
		return false, err
	}
	resp, err := client.ResolveExports(ctx, buildserver.ResolveExportRequest{
		Package:        util.FindFlagValue(ip.compileArgs, "-p"),
		Imports:        missing,
		UsePGO:         usePGO,
		Nested:         ip.nested,
		ResolvingChain: chain,
	})
	if err != nil {
		ip.Debug("build server resolve-export failed; falling back", "imports", missing, "error", err)
		return false, buildserver.ErrUnavailable
	}
	for _, importPath := range missing {
		if errMsg, failed := resp.Failed[importPath]; failed {
			return false, ex.Newf("resolving %q: %s", importPath, errMsg)
		}
		archives, ok := resp.Closures[importPath]
		if !ok {
			return false, ex.Newf("resolving %q: package not found or has no export file", importPath)
		}
		if !validateExportClosure(archives, ip.importConfig.PackageFile) {
			ip.Debug("build server returned incompatible or missing export archive; falling back", "import", importPath)
			return false, buildserver.ErrUnavailable
		}
	}
	*memoHits += resp.CacheHits
	*goListCalls += resp.GoListCalls
	var updated bool
	for _, importPath := range missing {
		if ip.mergeImportArchives(resp.Closures[importPath]) {
			updated = true
		}
	}
	return updated, nil
}

func resolveLinkExportsViaServer(
	ctx context.Context,
	client *buildserver.Client,
	roots []string,
	usePGO bool,
	existingPackageFile map[string]string,
) (linkResolveResult, error) {
	resp, err := client.ResolveExports(ctx, buildserver.ResolveExportRequest{
		Imports: roots,
		UsePGO:  usePGO,
		Nested:  false,
	})
	if err != nil {
		return linkResolveResult{}, buildserver.ErrUnavailable
	}
	for _, closure := range resp.Closures {
		if !validateExportClosure(closure, existingPackageFile) {
			return linkResolveResult{}, buildserver.ErrUnavailable
		}
	}
	failed := make(map[string]error, len(resp.Failed))
	for path, msg := range resp.Failed {
		failed[path] = ex.New(msg)
	}
	return linkResolveResult{
		closures:    resp.Closures,
		failed:      failed,
		cacheHits:   resp.CacheHits,
		goListCalls: resp.GoListCalls,
	}, nil
}

func validateExportClosure(closure, existingPackageFile map[string]string) bool {
	for pkg, archive := range closure {
		if existing, ok := existingPackageFile[pkg]; ok && existing != archive {
			return false
		}
		if _, err := os.Stat(archive); err != nil {
			return false
		}
	}
	return true
}
