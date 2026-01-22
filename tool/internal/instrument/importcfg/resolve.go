// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package importcfg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// PackageInfo represents the relevant fields from `go list -json` output.
type PackageInfo struct {
	ImportPath string   `json:"ImportPath"`
	Export     string   `json:"Export"`
	Deps       []string `json:"Deps"`
}

// ResolvePackageFiles attempts to retrieve the archive for the designated import path
// and its dependencies using `go list -export -json`.
func ResolvePackageFiles(ctx context.Context, importPath string) (map[string]string, error) {
	// Use go list to find the package and its dependencies
	cmd := exec.CommandContext(ctx, "go", "list", "-export", "-json", "-deps", importPath)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("go list failed: %w\nstderr: %s", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("go list failed: %w", err)
	}

	result := make(map[string]string)
	decoder := json.NewDecoder(strings.NewReader(string(output)))

	// go list -json outputs one JSON object per line
	for decoder.More() {
		var pkg PackageInfo
		if err2 := decoder.Decode(&pkg); err2 != nil {
			return nil, fmt.Errorf("decoding package info: %w", err2)
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
