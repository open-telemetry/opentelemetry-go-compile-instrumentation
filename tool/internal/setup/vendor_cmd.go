// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"slices"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/util"
)

// Vendor adds the modules of the otelc hook code to the vendor directory of
// the current module, for all time. After it, `otelc go build` needs no
// network and no module cache (for example in an air-gapped CI), and a plain
// `go build -mod=vendor` continues to work.
//
// It generates or updates otel.instrumentation.go (like `otelc pin`), then it
// merges the modules of the hook code into vendor/ and go.mod (see
// vendorSync). The changes are not reverted. args are go build flags and
// package patterns, as for `otelc pin`.
//
// Vendor acts like "go mod vendor", but for the modules of the hook code too:
// when the hook code needs a higher version of a vendored module, Vendor
// gets this version, replaces the vendored files of the module, and changes
// the requirement in go.mod. It prints each change.
func Vendor(ctx context.Context, args []string) error {
	return withBuildLock(ctx, func(ctx context.Context) error {
		return vendorLocked(ctx, args)
	})
}

func vendorLocked(ctx context.Context, args []string) (err error) {
	// The directory of the build is the -C directory of args, else the otelc
	// work directory, as for package loading.
	workDir, err := buildWorkDir(subcmdBuild, args)
	if err != nil {
		return ex.Wrapf(err, "otelc vendor")
	}
	root := vendoredModuleRoot(ctx, workDir)
	if root == "" {
		return ex.Newf("otelc vendor: the module in %s does not vendor its dependencies "+
			"(no vendor/modules.txt, or workspace mode); run \"go mod vendor\" first", workDir)
	}
	if err = requireVendorGoVersion(root); err != nil {
		return ex.Wrapf(err, "otelc vendor")
	}
	if mode := lastModFlag(subcmdBuild, args); mode != "" && mode != vendorDirName {
		return ex.Newf("otelc vendor: -mod=%s is not supported: otelc vendor uses the vendor directory "+
			"(-mod=vendor); remove the -mod flag from the arguments and from GOFLAGS", mode)
	}
	if hasModfileFlag(args) {
		return ex.Newf("otelc vendor: -modfile is not supported: otelc vendor changes go.mod; " +
			"remove the -modfile flag from the arguments and from GOFLAGS")
	}

	// A build that stopped before its cleanup left changed files. Restore
	// them first: else a later recovery removes the changes of otelc vendor.
	if err = recoverLeftoverState(ctx); err != nil {
		return err
	}

	// The go commands that load the packages of the user use vendor/: the go
	// version of go.mod is 1.14 or higher, and no -mod flag is set (else the
	// checks above fail). Vendor mode is forced in the context too: otelc
	// must not tidy the module of the user.
	ctx = contextWithVendorMode(ctx, true)
	ctx = contextWithVendorUpdate(ctx)

	// All the changes are tracked. If otelc vendor fails, they are reverted,
	// so the project does not stay half changed. If it succeeds, they stay.
	stateManager := newStateManager()
	ctx = contextWithStateManager(ctx, stateManager)
	defer func() {
		if err == nil {
			err = finishVendor(ctx, stateManager)
			if err == nil {
				return
			}
		}
		err = ex.Join(err, revertState(stateManager))
	}()

	moduleDirs, err := vendorModuleDirs(ctx, args)
	if err != nil {
		return ex.Wrapf(err, "otelc vendor")
	}
	backupFiles, err := getBackupFiles(ctx, moduleDirs)
	if err != nil {
		return ex.Wrapf(err, "otelc vendor")
	}
	if err = stateManager.TrackAll(backupFiles...); err != nil {
		return ex.Wrapf(err, "otelc vendor")
	}

	generate := false
	_, err = Pin(ctx, PinOptions{
		Prune: true,
		// `go tool otelc` needs a tool directive, which otelc does not add in
		// vendor mode. So the //go:generate directive is not added.
		Generate:   &generate,
		Args:       args,
		ModuleDirs: moduleDirs,
	})
	if err != nil {
		return ex.Wrapf(err, "otelc vendor")
	}
	return nil
}

// finishVendor removes the state of a successful otelc vendor. The manifest
// must go: else a later recovery reverts the changes of otelc vendor. If it
// cannot go, the result is an error, and the caller reverts the changes
// while the snapshots are there. Snapshots without a manifest are not used,
// so a failure to remove them is only a warning.
func finishVendor(ctx context.Context, stateManager *stateManager) error {
	discardErr := stateManager.Discard()
	if discardErr == nil {
		return nil
	}
	if lexists(util.GetBuildTemp(stateFileName)) {
		return ex.Wrapf(discardErr, "otelc vendor")
	}
	util.LoggerFromContext(ctx).WarnContext(ctx, "cannot remove the old snapshots", "error", discardErr)
	return nil
}

// vendorModuleDirs returns the module directories of the packages in args.
func vendorModuleDirs(ctx context.Context, args []string) ([]string, error) {
	buildPkgs, _, err := getBuildPackages(ctx, subcmdBuild, args)
	if err != nil {
		return nil, err
	}
	return pkgload.FindModuleDirs(ctx, buildPkgs)
}

// hasModfileFlag reports whether args or GOFLAGS have a -modfile flag.
func hasModfileFlag(args []string) bool {
	for _, tok := range util.SplitGoflags(os.Getenv("GOFLAGS")) {
		if isModfileFlag(util.UnquoteGoflagsToken(tok)) {
			return true
		}
	}
	return slices.ContainsFunc(args, isModfileFlag)
}
