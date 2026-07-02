// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
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

// yamlRule is the subset of a single rule entry we need to read the target/version from.
type yamlRule struct {
	Target  string `yaml:"target"`
	Version string `yaml:"version"`
}

// InstrumentedTargets walks rulesRoot, parses every *.yaml file as an
// instrumentation rule set, and returns instrumented targets mapped
// to their supported version ranges.
func InstrumentedTargets(t *testing.T, rulesRoot string) map[string][]string {
	targets := map[string][]string{}
	err := filepath.WalkDir(rulesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		data, readErr := os.ReadFile(path) //nolint:gosec
		require.NoError(t, readErr, "read rule file %s", path)

		var rules map[string]yamlRule
		require.NoError(t, yaml.Unmarshal(data, &rules), "parse rule file %s", path)

		for _, r := range rules {
			if r.Target != "" {
				targets[r.Target] = append(targets[r.Target], r.Version)
			}
		}
		return nil
	})
	require.NoError(t, err, "walk rules root %s", rulesRoot)
	require.NotEmpty(t, targets, "no instrumentation rule targets found under %s", rulesRoot)
	return targets
}

// directNonReplacedRequires returns the direct requires of the go.mod at
// appDir, excluding modules replaced by a local path.
func directNonReplacedRequires(t *testing.T, appDir string) []string {
	cmd := exec.CommandContext(t.Context(), "go", "mod", "edit", "-json")
	cmd.Dir = appDir
	out, err := cmd.Output()
	require.NoError(t, err, "go mod edit -json failed in %s", appDir)

	var mod goModJSON
	require.NoError(t, json.Unmarshal(out, &mod), "parse go mod edit -json in %s", appDir)

	localReplaces := make(map[string]bool, len(mod.Replace))
	for _, r := range mod.Replace {
		if strings.HasPrefix(r.New.Path, ".") || filepath.IsAbs(r.New.Path) {
			localReplaces[r.Old.Path] = true
		}
	}

	var requires []string
	for _, req := range mod.Require {
		if req.Indirect || localReplaces[req.Path] {
			continue
		}
		requires = append(requires, req.Path)
	}
	return requires
}

// DiscoverInstrumentedDeps returns the direct, non-replaced third-party
// requires of the go.mod at appDir that are covered by at least one
// instrumentation rule target + supported version range.
func DiscoverInstrumentedDeps(t *testing.T, appDir string, targets map[string][]string) []string {
	var deps []string
	for _, reqPath := range directNonReplacedRequires(t, appDir) {
		versionRanges := findMatchingVersionRanges(reqPath, targets)
		if len(versionRanges) == 0 {
			continue
		}

		cmd := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Version}}", reqPath+"@latest")
		out, err := cmd.Output()
		require.NoError(t, err, "go list -m -f {{.Version}} %s@latest failed in %s", reqPath, appDir)

		latestVersion := strings.TrimSpace(string(out))
		if coversAnyVersionRange(latestVersion, versionRanges) {
			deps = append(deps, reqPath)
		}
	}
	return deps
}

// findMatchingVersionRanges returns all version ranges for all instrumented targets
// whose module path matches requirePath.
// A module covers a target when the target equals the module path
// or is rooted at it (target == path or target starts with path+"/").
func findMatchingVersionRanges(requirePath string, targets map[string][]string) map[string]bool {
	allVersionRanges := map[string]bool{}
	prefix := requirePath + "/"
	for target, versionRanges := range targets {
		if target == requirePath || strings.HasPrefix(target, prefix) {
			for _, vr := range versionRanges {
				allVersionRanges[vr] = true
			}
		}
	}
	return allVersionRanges
}

// coversAnyVersionRange reports whether version is included in any
// of the provided version ranges.
func coversAnyVersionRange(version string, versionRanges map[string]bool) bool {
	for vr := range versionRanges {
		if util.VersionInRange(version, vr) {
			return true
		}
	}
	return false
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
