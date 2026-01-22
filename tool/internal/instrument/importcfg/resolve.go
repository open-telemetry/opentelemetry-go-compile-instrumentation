// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package importcfg

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// PackageInfo represents the relevant fields from `go list -json` output.
type PackageInfo struct {
	ImportPath string
	Export     string
	Deps       []string
}

// ResolvePackageFiles attempts to retrieve the archive for the designated import path
// and its dependencies using `go list -export -json`.
func ResolvePackageFiles(ctx context.Context, importPath string) (map[string]string, error) {
	// Use go list to find the package and its dependencies
	cmd := exec.CommandContext(ctx, "go", "list", "-export", "-json", "-deps", importPath)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list failed: %w\nstderr: %s", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("go list failed: %w", err)
	}

	result := make(map[string]string)
	decoder := json.NewDecoder(strings.NewReader(string(output)))

	// go list -json outputs one JSON object per line
	for decoder.More() {
		var pkg PackageInfo
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("decoding package info: %w", err)
		}

		// Only include packages that have an export archive
		if pkg.Export != "" {
			result[pkg.ImportPath] = pkg.Export
		}
	}

	// Verify we found the requested package
	if _, found := result[importPath]; !found {
		return nil, fmt.Errorf("package %q not found in go list output", importPath)
	}

	return result, nil
}
