// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// envUnlockedCompile enables compiling after the build lock is released.
const envUnlockedCompile = "OTELC_UNLOCKED_COMPILE"

const (
	invocationsDir = "invocations"
	overlayDir     = "overlay"
)

// setupSnapshot is what a build needs to compile after it released the build
// lock: an -overlay with the files setup changed in the source tree, and a
// directory with the files toolexec reads from .otelc-build.
type setupSnapshot struct {
	invocationDir string
	overlayFile   string
}

// canBuildUnlocked reports whether the compile of this build can run outside
// the build lock. Builds that read setup's changes some other way than through
// the go command keep the lock until they finish.
func canBuildUnlocked(ctx context.Context, cmd *cli.Command, vendored bool) bool {
	if os.Getenv(envUnlockedCompile) != "1" {
		return false
	}
	args := cmd.Args().Slice()
	subcommand := args[0]
	if subcommand != subcmdBuild && subcommand != subcmdInstall {
		// go test runs the test binaries, which can start nested builds that
		// expect the files of their parent in the tree.
		return false
	}
	if vendored || cmd.Bool("debug") {
		return false
	}
	flags := slices.Concat(args[1:], util.SplitGoflags(os.Getenv("GOFLAGS")))
	for _, arg := range flags {
		name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if name == "overlay" || name == "modfile" {
			return false
		}
	}
	if _, fileTargets, err := splitBuildTargets(subcommand, args[1:]); err != nil || len(fileTargets) > 0 {
		return false
	}
	stateManager, found := stateManagerFromContext(ctx)
	if !found {
		return false
	}
	for path := range stateManager.files {
		if filepath.Base(path) == "go.work.sum" {
			return false
		}
	}
	return true
}

// snapshotSetup copies the files setup wrote for this build out of the way
// of the next setup. It must run under the build lock, before Cleanup.
func snapshotSetup(ctx context.Context) (*setupSnapshot, error) {
	stateManager, _ := stateManagerFromContext(ctx)

	parent := util.GetBuildTemp(invocationsDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, ex.Wrapf(err, "creating %s", parent)
	}
	dir, err := os.MkdirTemp(parent, "build-")
	if err != nil {
		return nil, ex.Wrapf(err, "creating the invocation directory")
	}
	snap := &setupSnapshot{invocationDir: dir, overlayFile: filepath.Join(dir, "overlay.json")}

	if err = snap.copyBuildState(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err = snap.writeOverlay(stateManager); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return snap, nil
}

// copyBuildState copies the files that toolexec reads through
// util.GetInvocationTemp.
func (s *setupSnapshot) copyBuildState() error {
	for _, src := range []string{
		util.GetMatchedRuleFile(),
		util.GetImportNamesFile(),
		util.GetPGOProfileFile(),
		util.GetInjectedHashFile(),
		util.GetResolutionScopeFile(),
		util.GetSetupGenerationFile(),
	} {
		if !util.PathExists(src) {
			continue
		}
		if err := util.CopyFile(src, filepath.Join(s.invocationDir, filepath.Base(src))); err != nil {
			return err
		}
	}
	return nil
}

// writeOverlay maps each file setup created or changed to a copy of its
// content, and each file setup removed to nothing. The copies are named by
// their content, so builds with the same setup output share them.
func (s *setupSnapshot) writeOverlay(stateManager *stateManager) error {
	replace := make(map[string]string, len(stateManager.files))
	for path, existed := range stateManager.files {
		if !util.PathExists(path) {
			if existed {
				replace[path] = ""
			}
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return ex.Wrapf(err, "reading %s", path)
		}
		sum := sha256.Sum256(data)
		dst := filepath.Join(util.GetBuildTemp(overlayDir), hex.EncodeToString(sum[:]), filepath.Base(path))
		if !util.PathExists(dst) {
			if err = os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return ex.Wrapf(err, "creating %s", filepath.Dir(dst))
			}
			if err = util.WriteFileAtomic(dst, data); err != nil {
				return err
			}
		}
		replace[path] = dst
	}
	content, err := json.Marshal(map[string]map[string]string{"Replace": replace})
	if err != nil {
		return ex.Wrapf(err, "encoding the overlay")
	}
	return util.WriteFileAtomic(s.overlayFile, content)
}

// compileWith returns the compile step that runs outside the build lock.
func (s *setupSnapshot) compileWith(
	compile func(ctx context.Context, extraFlags ...string) error,
) func(context.Context) error {
	return func(ctx context.Context) error {
		defer os.RemoveAll(s.invocationDir)
		if err := os.Setenv(util.EnvOtelcInvocationDir, s.invocationDir); err != nil {
			return ex.Wrapf(err, "setting %s", util.EnvOtelcInvocationDir)
		}
		defer os.Unsetenv(util.EnvOtelcInvocationDir)
		return compile(ctx, "-overlay="+s.overlayFile)
	}
}
