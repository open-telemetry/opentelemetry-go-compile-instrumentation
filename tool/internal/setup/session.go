// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/util"
)

// cleanupOwnedSession removes the session directory of a build that owns its
// session: one that inherits none from an outer otelc build. An inherited
// session belongs to the outer build and is never cleaned here.
func cleanupOwnedSession() error {
	if os.Getenv(util.EnvOtelcSessionDir) != "" {
		return nil
	}
	return os.RemoveAll(util.GetBuildTemp(util.SessionTempDir))
}

// sessionEnv adds the session directory to the env of the go command an
// owning build runs, so its toolexec processes and any otelc build its
// binaries run inherit the session.
func sessionEnv(env []string) []string {
	if os.Getenv(util.EnvOtelcSessionDir) != "" {
		return env
	}
	return append(env, util.EnvOtelcSessionDir+"="+util.GetBuildTemp(util.SessionTempDir))
}

// writeResolutionScope records the digest that scopes the export memo: the
// otelc version, the matched rules, the module files of moduleDirs after
// setup's edits, the go.work and go.work.sum each module directory actually
// resolves (an implicit workspace in a parent directory included), and the
// build environment. buildDir is the directory the go command of the build
// runs in (its -C directory, else the current directory), not otelc's work
// directory: go picks an implicit go.work from there, so it is resolved too.
// Builds with any other input never share memo entries.
func writeResolutionScope(ctx context.Context, buildDir string, moduleDirs []string) error {
	// The files of a module directory whose content decides which
	// dependency versions a build resolves.
	moduleFiles := []string{"go.mod", "go.sum", "go.work", "go.work.sum"}
	// The environment variables that change what a nested go list resolves.
	envVars := []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT", "GOFLAGS", "GOTOOLCHAIN", "GOWORK"}

	h := sha256.New()
	hashFile := func(path string) {
		content, err := os.ReadFile(path)
		if err != nil {
			return
		}
		sum := sha256.Sum256(content)
		_, _ = h.Write([]byte(path + "=" + hex.EncodeToString(sum[:]) + "\x00"))
	}
	_, _ = h.Write([]byte("otelc=" + util.Version + "\x00rules=" + instrument.RulesDigest() + "\x00"))
	hashWorkspace := func(label, dir string) error {
		goWork, err := resolvedGoWork(ctx, dir)
		if err != nil {
			return err
		}
		_, _ = h.Write([]byte(label + ":gowork=" + goWork + "\x00"))
		if goWork != "" && goWork != goWorkOff {
			hashFile(goWork)
			hashFile(goWork + ".sum")
		}
		return nil
	}
	if err := hashWorkspace("build", buildDir); err != nil {
		return err
	}
	for _, dir := range moduleDirs {
		for _, name := range moduleFiles {
			hashFile(filepath.Join(dir, name))
		}
		if err := hashWorkspace(dir, dir); err != nil {
			return err
		}
	}
	for _, name := range envVars {
		_, _ = h.Write([]byte(name + "=" + os.Getenv(name) + "\x00"))
	}
	f := util.GetResolutionScopeFile()
	if err := util.WriteFileAtomic(f, []byte(hex.EncodeToString(h.Sum(nil))[:32])); err != nil {
		return ex.Wrapf(err, "failed to write resolution scope to file %s", f)
	}
	return nil
}

// resolvedGoWork returns the go.work file go resolves for dir: the GOWORK
// value or the nearest go.work above dir, "off" when workspaces are
// disabled, or "" when none applies.
func resolvedGoWork(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "GOWORK")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", ex.Wrapf(err, "running go env GOWORK in %s", dir)
	}
	return strings.TrimSpace(string(out)), nil
}

// buildCommandDir returns the directory the build's go command runs in: the
// value of the last change-directory flag in buildFlags (-C dir, -C=dir,
// --C dir, or --C=dir) made absolute, else the current directory. The flags
// are classified first, so a value that merely looks like a change-directory
// flag, such as the one in -tags -C=x, is never taken for one.
func buildCommandDir(buildFlags []string) (string, error) {
	dir := ""
	classified := classifyArgs(subcmdBuild, buildFlags)
	for i, a := range classified {
		if a.Kind != ArgBuildFlag || a.FlagName != "-C" {
			continue
		}
		switch {
		case a.HasValue:
			dir = a.Value
		case i+1 < len(classified) && classified[i+1].Kind == ArgBuildFlagValue:
			dir = classified[i+1].Raw
		}
	}
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", ex.Wrapf(err, "getting the build directory")
		}
		return wd, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", ex.Wrapf(err, "resolving -C directory %s", dir)
	}
	return abs, nil
}
