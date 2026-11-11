// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/util"
)

// scopeEnvVars are the environment variables that change what a nested
// `go list -export` resolves, so they are part of the resolution scope.
//
//nolint:gochecknoglobals // read-only lookup table
var scopeEnvVars = []string{
	"GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT", "GOFLAGS", "GOTOOLCHAIN", "GOWORK",
}

// scopeModuleFiles are the files of a module directory whose content decides
// which dependency versions a build resolves.
//
//nolint:gochecknoglobals // read-only lookup table
var scopeModuleFiles = []string{"go.mod", "go.sum", "go.work", "go.work.sum"}

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
// setup's edits, and the build environment. Builds with any other input
// never share memo entries.
func writeResolutionScope(moduleDirs []string) error {
	h := sha256.New()
	_, _ = h.Write([]byte("otelc=" + util.Version + "\x00rules=" + instrument.RulesDigest() + "\x00"))
	for _, dir := range moduleDirs {
		for _, name := range scopeModuleFiles {
			content, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			sum := sha256.Sum256(content)
			_, _ = h.Write([]byte(filepath.Join(dir, name) + "=" + hex.EncodeToString(sum[:]) + "\x00"))
		}
	}
	for _, name := range scopeEnvVars {
		_, _ = h.Write([]byte(name + "=" + os.Getenv(name) + "\x00"))
	}
	f := util.GetResolutionScopeFile()
	if err := util.WriteFileAtomic(f, []byte(hex.EncodeToString(h.Sum(nil))[:32])); err != nil {
		return ex.Wrapf(err, "failed to write resolution scope to file %s", f)
	}
	return nil
}
