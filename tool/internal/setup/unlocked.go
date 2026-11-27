// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

const envUnlockedCompile = "OTELC_UNLOCKED_COMPILE"

type goOverlay struct {
	Replace map[string]string `json:"Replace"`
}

// setupSnapshot owns both the extracted bundle and all per-build metadata.
// It lives outside .otelc-build so cleanup and another tool version cannot
// remove inputs still in use by an unlocked compiler.
type setupSnapshot struct {
	invocationDir string
	overlayFile   string
}

func unlockedCompileEnabled() bool {
	return os.Getenv(envUnlockedCompile) == "1"
}

func canBuildUnlocked(ctx context.Context, cmd *cli.Command, vendored bool) bool {
	if !unlockedCompileEnabled() || vendored || cmd.Bool("debug") || util.InheritedSession() {
		return false
	}
	args := cmd.Args().Slice()
	if len(args) == 0 || (args[0] != subcmdBuild && args[0] != subcmdInstall) {
		return false
	}
	goflags := util.SplitGoflags(os.Getenv("GOFLAGS"))
	for index, token := range goflags {
		goflags[index] = util.UnquoteGoflagsToken(token)
	}
	flags := slices.Concat(goflags, args[1:])
	for _, arg := range classifyArgs(args[0], flags) {
		switch arg.FlagName {
		case flagOverlay, flagModfile:
			return false
		case flagMod:
			if (arg.HasValue || arg.Kind == ArgBuildFlagValue) && arg.Value != "readonly" {
				return false
			}
		}
	}
	if _, fileTargets, err := splitBuildTargets(args[0], args[1:]); err != nil || len(fileTargets) > 0 {
		return false
	}
	state, found := stateManagerFromContext(ctx)
	if !found {
		return false
	}
	for path := range state.files {
		if filepath.Base(path) == "go.work.sum" {
			return false
		}
	}
	return true
}

func newSetupSnapshot() (*setupSnapshot, error) {
	dir, err := os.MkdirTemp("", "otelc-invocation-")
	if err != nil {
		return nil, ex.Wrap(err)
	}
	return &setupSnapshot{invocationDir: dir, overlayFile: filepath.Join(dir, "overlay.json")}, nil
}

// writeOverlay uses the same file-copy and snapshot naming helpers as Track.
// Copies are invocation-owned, including changed files inside the bundle:
// Revert may restore those files before the compiler starts reading them.
func (s *setupSnapshot) writeOverlay(state *stateManager) error {
	replace := make(map[string]string, len(state.files))
	for path, existed := range state.files {
		_, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			if existed {
				replace[path] = ""
			}
			continue
		}
		if err != nil {
			return ex.Wrap(err)
		}
		dst := filepath.Join(s.invocationDir, "overlay", stateSnapshotPath(path), filepath.Base(path))
		if err = util.CopyFile(path, dst); err != nil {
			return err
		}
		replace[path] = dst
	}
	content, err := json.Marshal(goOverlay{Replace: replace})
	if err != nil {
		return ex.Wrap(err)
	}
	return util.WriteFileAtomic(s.overlayFile, content)
}

// restoreBeforeCompile must succeed before the module lock is released.
// Unlike Cleanup, it propagates errors and retains recovery state on failure.
func restoreBeforeCompile(state *stateManager) error {
	if err := state.Revert(); err != nil {
		return err
	}
	return state.Discard()
}

func (s *setupSnapshot) compileWith(
	compile func(ctx context.Context, extraFlags ...string) error,
) func(context.Context) error {
	return func(ctx context.Context) error {
		// Setup already tidied the module. Do not allow the unlocked Go command
		// to write module metadata if it discovers another dependency.
		return compile(ctx, "-mod=readonly", "-overlay="+s.overlayFile)
	}
}
