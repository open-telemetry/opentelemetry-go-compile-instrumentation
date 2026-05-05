// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// goModJSON is the subset of `go mod edit -json` output we care about.
type goModJSON struct {
	Require []struct {
		Path     string
		Indirect bool
	}
	Replace []struct {
		Old struct{ Path string }
		New struct{ Path string }
	}
}

// DiscoverDirectDeps returns the direct, non-replaced third-party module paths
// declared in the go.mod at appDir. Modules overridden by a local-filesystem
// replace directive (path starting with "." or "/") are excluded, as are
// indirect dependencies. Stdlib-only apps return an empty slice.
func DiscoverDirectDeps(t *testing.T, appDir string) []string {
	cmd := exec.CommandContext(t.Context(), "go", "mod", "edit", "-json")
	cmd.Dir = appDir
	out, err := cmd.Output()
	require.NoError(t, err, "go mod edit -json failed in %s", appDir)

	var mod goModJSON
	require.NoError(t, json.Unmarshal(out, &mod), "parse go mod edit -json in %s", appDir)

	localReplaces := make(map[string]bool, len(mod.Replace))
	for _, r := range mod.Replace {
		if strings.HasPrefix(r.New.Path, ".") || strings.HasPrefix(r.New.Path, "/") {
			localReplaces[r.Old.Path] = true
		}
	}

	var deps []string
	for _, req := range mod.Require {
		if !req.Indirect && !localReplaces[req.Path] {
			deps = append(deps, req.Path)
		}
	}
	return deps
}

// BumpToLatest runs "go get <dep>@latest" for each dep in appDir, followed by "go mod tidy".
// CAUTION: The go.mod and go.sum files in appDir are modified in place.
func BumpToLatest(t *testing.T, appDir string, deps ...string) {
	for _, dep := range deps {
		t.Logf("bumping %s in %s to @latest", dep, appDir)
		cmd := exec.CommandContext(t.Context(), "go", "get", dep+"@latest") //nolint:gosec
		cmd.Dir = appDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "go get %s@latest failed in %s:\n%s", dep, appDir, string(out))
	}

	tidyCmd := exec.CommandContext(t.Context(), "go", "mod", "tidy")
	tidyCmd.Dir = appDir
	out, err := tidyCmd.CombinedOutput()
	require.NoError(t, err, "go mod tidy failed in %s:\n%s", appDir, string(out))
}
