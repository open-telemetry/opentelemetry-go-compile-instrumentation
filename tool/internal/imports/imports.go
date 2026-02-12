// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package imports provides utilities for managing Go import declarations in AST files.
package imports

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/token"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

// ResolvePackageName resolves the package name for an import path using `go list -f '{{.Name}}'`.
// Results are cached for performance. Panics if the package cannot be resolved, as this
// indicates a serious issue during compilation (packages should always be resolvable
// during toolexec since the Go toolchain has already resolved all dependencies).
func ResolvePackageName(ctx context.Context, importPath string, buildFlags ...string) string {
	// Build the go list command with any build flags
	args := make([]string, 0, 3+len(buildFlags)+1)
	args = append(args, "list", "-f", "{{.Name}}")
	args = append(args, buildFlags...)
	args = append(args, importPath)

	cmd := exec.CommandContext(ctx, "go", args...)
	output, err := cmd.Output()
	if err != nil {
		ex.Fatalf("failed to resolve package name for %s: %v", importPath, err)
	}

	name := strings.TrimSpace(string(output))
	if name == "" {
		ex.Fatalf("go list returned empty package name for %s", importPath)
	}

	return name
}

// packageInfo represents the relevant fields from `go list -json` output.
type packageInfo struct {
	ImportPath string   `json:"ImportPath"`
	Export     string   `json:"Export"`
	Deps       []string `json:"Deps"`
}

// ResolvePackageInfo attempts to retrieve the archive for the designated import path
// and its dependencies using `go list -export -json`.
// The buildFlags parameter allows passing build flags (like -tags) to ensure the resolved
// archives match the current build context.
func ResolvePackageInfo(ctx context.Context, importPath string, buildFlags ...string) (map[string]string, error) {
	// Build the go list command with any build flags
	args := make([]string, 0, 4+len(buildFlags)+1)
	args = append(args, "list", "-export", "-json", "-deps")
	args = append(args, buildFlags...)
	args = append(args, importPath)

	// Use go list to find the package and its dependencies
	cmd := exec.CommandContext(ctx, "go", args...)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, ex.Wrapf(err, "go list failed\nstderr: %s", string(exitErr.Stderr))
		}
		return nil, ex.Wrapf(err, "go list failed")
	}

	result := make(map[string]string)
	decoder := json.NewDecoder(bytes.NewReader(output))

	// go list -json outputs one JSON object per line
	for {
		var pkg packageInfo
		if err = decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, ex.Wrapf(err, "decoding package info")
		}

		// Only include packages that have an export archive
		if pkg.Export != "" {
			result[pkg.ImportPath] = pkg.Export
		}
	}

	// Verify we found the requested package
	if _, found := result[importPath]; !found {
		return nil, ex.Newf("package %q not found in go list output", importPath)
	}

	return result, nil
}

// importMapping holds bidirectional import mappings for a file.
type importMapping struct {
	AliasToPath   map[string]string // alias -> import path
	PathToAlias   map[string]string // import path -> alias
	ExplicitAlias map[string]bool   // import path -> true if alias was explicitly set
}

// Resolution contains the result of analyzing which imports need to be added.
type Resolution struct {
	NewImports      map[string]string // Imports to add (alias -> path)
	ExistingAliases map[string]string // Existing imports in file (path -> alias)
	ExplicitAliases map[string]bool   // path -> true if the existing alias was explicitly set
}

// parseFile extracts all imports from a file into bidirectional maps.
// This avoids multiple AST traversals when checking import conflicts.
//
// For imports without explicit aliases, the alias is resolved using ResolvePackageName(),
// which uses `go list` to get the actual package name. The ExplicitAlias map tracks
// which imports have user-specified aliases in the source file.
func parseFile(ctx context.Context, root *dst.File, buildFlags ...string) importMapping {
	maps := importMapping{
		AliasToPath:   make(map[string]string),
		PathToAlias:   make(map[string]string),
		ExplicitAlias: make(map[string]bool),
	}

	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		for _, spec := range genDecl.Specs {
			importSpec, isImport := spec.(*dst.ImportSpec)
			if !isImport || importSpec.Path == nil {
				continue
			}
			importPath := strings.Trim(importSpec.Path.Value, `"`)

			var alias string
			if importSpec.Name != nil {
				alias = importSpec.Name.Name
				maps.ExplicitAlias[importPath] = true
			} else {
				alias = ResolvePackageName(ctx, importPath, buildFlags...)
			}

			maps.AliasToPath[alias] = importPath
			maps.PathToAlias[importPath] = alias
		}
	}
	return maps
}

// FindNew determines which imports from the rule are not already present in the file.
// It returns both the new imports to add and the existing aliases for conflict detection.
func FindNew(ctx context.Context, root *dst.File, ruleImports map[string]string, buildFlags ...string) Resolution {
	result := Resolution{
		NewImports:      make(map[string]string),
		ExistingAliases: make(map[string]string),
		ExplicitAliases: make(map[string]bool),
	}

	if len(ruleImports) == 0 {
		return result
	}

	existing := parseFile(ctx, root, buildFlags...)
	result.ExistingAliases = existing.PathToAlias
	result.ExplicitAliases = existing.ExplicitAlias

	for alias, importPath := range ruleImports {
		if alias == "_" || alias == "." {
			// Blank/dot imports can appear multiple times
			if _, pathExists := existing.PathToAlias[importPath]; !pathExists {
				result.NewImports[alias] = importPath
			}
			continue
		}

		if _, pathExists := existing.PathToAlias[importPath]; !pathExists {
			result.NewImports[alias] = importPath
		}
		// If path exists (even with different alias), skip - don't add duplicate
	}

	return result
}

// getExisting returns a map of alias -> path for all imports in the file.
// For imports without explicit aliases, the package name is used as the key.
func getExisting(ctx context.Context, root *dst.File, buildFlags ...string) map[string]string {
	return parseFile(ctx, root, buildFlags...).AliasToPath
}

// createSpec creates an ImportSpec with proper alias handling.
func createSpec(ctx context.Context, alias, importPath string, buildFlags ...string) *dst.ImportSpec {
	spec := &dst.ImportSpec{
		Path: &dst.BasicLit{Value: strconv.Quote(importPath)},
	}

	pkgName := ResolvePackageName(ctx, importPath, buildFlags...)

	// Set Name only if:
	// 1. It's a blank import (alias == "_")
	// 2. It's an explicit alias different from the inferred package name
	if alias == "_" || alias != pkgName {
		spec.Name = dst.NewIdent(alias)
	}

	return spec
}

// findFirstDecl returns the first import declaration in the file, or nil if none exist.
func findFirstDecl(root *dst.File) *dst.GenDecl {
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if ok && genDecl.Tok == token.IMPORT {
			return genDecl
		}
	}
	return nil
}

// AddToFile adds import declarations to the AST file.
// It checks for conflicts and reuses existing import blocks when possible.
func AddToFile(ctx context.Context, root *dst.File, newImports map[string]string, buildFlags ...string) error {
	if len(newImports) == 0 {
		return nil
	}

	existingImports := getExisting(ctx, root, buildFlags...)

	// Create reverse lookup: path -> alias
	existingByPath := make(map[string]string)
	for alias, importPath := range existingImports {
		existingByPath[importPath] = alias
	}

	// Check for conflicts: same alias but different path, or same path with different alias
	for alias, newPath := range newImports {
		if alias == "_" || alias == "." {
			continue
		}
		if existingPath, exists := existingImports[alias]; exists {
			if existingPath != newPath {
				return ex.Newf("import conflict: alias %q already imports %q, cannot import %q",
					alias, existingPath, newPath)
			}
			delete(newImports, alias)
		} else if _, exists = existingByPath[newPath]; exists {
			delete(newImports, alias)
		}
	}

	if len(newImports) == 0 {
		return nil
	}

	// Sort aliases for deterministic output
	aliases := make([]string, 0, len(newImports))
	for alias := range newImports {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	importDecl := findFirstDecl(root)

	if importDecl != nil {
		for _, alias := range aliases {
			spec := createSpec(ctx, alias, newImports[alias], buildFlags...)
			importDecl.Specs = append(importDecl.Specs, spec)
		}
	} else {
		specs := make([]dst.Spec, 0, len(newImports))
		for _, alias := range aliases {
			spec := createSpec(ctx, alias, newImports[alias], buildFlags...)
			specs = append(specs, spec)
		}

		newImportDecl := &dst.GenDecl{
			Tok:   token.IMPORT,
			Specs: specs,
		}

		root.Decls = append([]dst.Decl{newImportDecl}, root.Decls...)
	}

	return nil
}

// CollectPaths returns a map of all unique import paths in the file.
// The map uses import path as both key and value, ensuring that multiple blank (_) or
// dot (.) imports don't collapse to a single entry.
func CollectPaths(ctx context.Context, root *dst.File, buildFlags ...string) map[string]string {
	existing := parseFile(ctx, root, buildFlags...)
	paths := make(map[string]string, len(existing.PathToAlias))
	for importPath := range existing.PathToAlias {
		paths[importPath] = importPath
	}
	return paths
}
