// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package imports provides utilities for managing Go import declarations in AST files.
package imports

import (
	"context"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
)

// importMapping holds bidirectional import mappings for a file.
type importMapping struct {
	AliasToPath   map[string]string   // alias -> import path
	PathToAlias   map[string]string   // import path -> one alias (last spec wins)
	PathToAliases map[string][]string // import path -> every alias, sorted
	ExplicitAlias map[string]bool     // import path -> true if alias was explicitly set
}

// Resolution contains the result of analyzing which imports need to be added.
type Resolution struct {
	NewImports        map[string]string   // Imports to add (alias -> path)
	ExistingAliases   map[string]string   // Existing imports in file (path -> one alias)
	ExistingAliasSets map[string][]string // Existing imports in file (path -> every alias, sorted)
	ExplicitAliases   map[string]bool     // path -> true if the existing alias was explicitly set
}

// parseFile extracts all imports from a file into bidirectional maps.
// This avoids multiple AST traversals when checking import conflicts.
//
// A file may import one path under multiple aliases. PathToAlias keeps one
// alias per path (the last spec wins); PathToAliases keeps every alias,
// sorted, so callers can check set membership.
//
// For imports without explicit aliases, the alias is the package name in
// resolvedNames (import path -> package name) when it has one. Otherwise it is
// resolved using pkgload.ResolvePackageName(), which uses the go/packages API to
// get the actual package name. The ExplicitAlias map tracks which imports have
// user-specified aliases in the source file.
func parseFile(
	ctx context.Context, root *dst.File, resolvedNames map[string]string, buildFlags ...string,
) importMapping {
	maps := importMapping{
		AliasToPath:   make(map[string]string),
		PathToAlias:   make(map[string]string),
		PathToAliases: make(map[string][]string),
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
			} else if name, resolved := resolvedNames[importPath]; resolved {
				alias = name
			} else {
				alias = pkgload.ResolvePackageName(ctx, importPath, buildFlags...)
			}

			maps.AliasToPath[alias] = importPath
			maps.PathToAlias[importPath] = alias
			maps.PathToAliases[importPath] = append(maps.PathToAliases[importPath], alias)
		}
	}
	for _, aliases := range maps.PathToAliases {
		slices.Sort(aliases)
	}
	return maps
}

// FindNew determines which imports from the rule are not already present in the file.
// It returns both the new imports to add and the existing aliases for conflict detection.
// resolvedNames maps an import path to its package name; see parseFile.
func FindNew(
	ctx context.Context, root *dst.File, ruleImports, resolvedNames map[string]string, buildFlags ...string,
) Resolution {
	result := Resolution{
		NewImports:        make(map[string]string),
		ExistingAliases:   make(map[string]string),
		ExistingAliasSets: make(map[string][]string),
		ExplicitAliases:   make(map[string]bool),
	}

	if len(ruleImports) == 0 {
		return result
	}

	existing := parseFile(ctx, root, resolvedNames, buildFlags...)
	result.ExistingAliases = existing.PathToAlias
	result.ExistingAliasSets = existing.PathToAliases
	result.ExplicitAliases = existing.ExplicitAlias

	for alias, importPath := range ruleImports {
		if alias == "_" || alias == "." {
			// Blank/dot imports can appear multiple times
			if _, pathExists := existing.PathToAlias[importPath]; !pathExists {
				result.NewImports[alias] = importPath
			}
			continue
		}

		// Add the import unless the file already imports the path under this
		// alias. Go accepts one path imported under several aliases.
		if !slices.Contains(existing.PathToAliases[importPath], alias) {
			result.NewImports[alias] = importPath
		}
	}

	return result
}

// Existing returns a map of alias -> path for all imports in the file.
// For imports without explicit aliases, the package name is used as the key.
func Existing(ctx context.Context, root *dst.File, buildFlags ...string) map[string]string {
	return parseFile(ctx, root, nil, buildFlags...).AliasToPath
}

// createSpec creates an ImportSpec with proper alias handling.
func createSpec(ctx context.Context, alias, importPath string, buildFlags ...string) *dst.ImportSpec {
	spec := &dst.ImportSpec{
		Path: &dst.BasicLit{Value: strconv.Quote(importPath)},
	}

	pkgName := pkgload.ResolvePackageName(ctx, importPath, buildFlags...)

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
// resolvedNames maps an import path to its package name; see parseFile.
func AddToFile(
	ctx context.Context, root *dst.File, newImports, resolvedNames map[string]string, buildFlags ...string,
) error {
	if len(newImports) == 0 {
		return nil
	}

	existingImports := parseFile(ctx, root, resolvedNames, buildFlags...).AliasToPath
	fileNames := fileScopeNames(root)

	// Check for conflicts: an alias that already imports another path, or an
	// alias that a declaration in the file already uses. A path the file
	// already imports under another alias is imported again under this one.
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
			continue
		}
		if fileNames[alias] {
			return ex.Newf("import conflict: alias %q for %q is already declared in the file",
				alias, newPath)
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
	slices.Sort(aliases)

	// A path the file already imports gets the alias written out, so its
	// package name does not have to be resolved.
	importedPaths := make(map[string]bool, len(existingImports))
	for _, importPath := range existingImports {
		importedPaths[importPath] = true
	}
	newSpec := func(alias string) *dst.ImportSpec {
		importPath := newImports[alias]
		if importedPaths[importPath] && alias != "_" && alias != "." {
			return &dst.ImportSpec{
				Name: dst.NewIdent(alias),
				Path: &dst.BasicLit{Value: strconv.Quote(importPath)},
			}
		}
		return createSpec(ctx, alias, importPath, buildFlags...)
	}

	importDecl := findFirstDecl(root)

	if importDecl != nil {
		for _, alias := range aliases {
			importDecl.Specs = append(importDecl.Specs, newSpec(alias))
		}
	} else {
		specs := make([]dst.Spec, 0, len(newImports))
		for _, alias := range aliases {
			specs = append(specs, newSpec(alias))
		}

		newImportDecl := &dst.GenDecl{
			Tok:   token.IMPORT,
			Specs: specs,
		}

		root.Decls = append([]dst.Decl{newImportDecl}, root.Decls...)
	}

	return nil
}

// fileScopeNames returns the names that root's top-level declarations bind:
// functions, types, variables, and constants. Methods and imports are left out.
func fileScopeNames(root *dst.File) map[string]bool {
	names := make(map[string]bool)
	for _, decl := range root.Decls {
		switch d := decl.(type) {
		case *dst.FuncDecl:
			if d.Recv == nil && d.Name != nil {
				names[d.Name.Name] = true
			}
		case *dst.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *dst.TypeSpec:
					names[sp.Name.Name] = true
				case *dst.ValueSpec:
					for _, name := range sp.Names {
						names[name.Name] = true
					}
				}
			}
		}
	}
	return names
}

// CollectImportPaths returns a map of all unique import paths declared in root
// without resolving package names. Both key and value are the import path string.
func CollectImportPaths(root *dst.File) map[string]string {
	if root == nil {
		return nil
	}
	paths := make(map[string]string)
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		for _, spec := range genDecl.Specs {
			importSpec, specOk := spec.(*dst.ImportSpec)
			if !specOk || importSpec.Path == nil {
				continue
			}
			p, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				p = strings.Trim(importSpec.Path.Value, `"`)
			}
			if p != "" {
				paths[p] = p
			}
		}
	}
	return paths
}
