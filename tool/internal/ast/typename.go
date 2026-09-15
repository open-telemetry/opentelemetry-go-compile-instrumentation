// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
)

// typeNameRe parses type-name strings of the form [*][pkg.]Name.
// It handles identifiers, qualified identifiers, and pointers to those.
// Limitations: does not handle chan, func, map, slice, or interface literals.
var typeNameRe = regexp.MustCompile(
	`\A(\*)?\s*(?:([A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*)\.)?([A-Za-z_][A-Za-z0-9_]*)\z`,
)

// parsedTypeName represents a parsed Go type expression.
type parsedTypeName struct {
	importPath string // package qualifier (e.g. "context"), empty for builtins
	name       string // leaf name (e.g. "Context", "error", "int")
	pointer    bool   // whether the type is a pointer
}

// parseTypeName parses a string like "error", "int", "context.Context", or
// "*http.Request" into a parsedTypeName.
func parseTypeName(s string) (parsedTypeName, error) {
	m := typeNameRe.FindStringSubmatch(s)
	if m == nil {
		return parsedTypeName{}, ex.Newf("invalid type name %q", s)
	}
	return parsedTypeName{pointer: m[1] == "*", importPath: m[2], name: m[3]}, nil
}

// matches reports whether the dst.Expr node represents this type. imports
// resolves the local identifier used at node's use site (an import alias, or
// the default package name when unaliased) to that package's real import
// path; see importAliasMap. It may be nil, e.g. for hand-built AST nodes in
// tests that have no backing *dst.File, in which case matching falls back to
// comparing against importPath's last segment.
func (t parsedTypeName) matches(node dst.Expr, imports map[string]string) bool {
	switch n := node.(type) {
	case *dst.Ident:
		return !t.pointer && t.importPath == n.Path && t.name == n.Name

	case *dst.SelectorExpr:
		ident, ok := n.X.(*dst.Ident)
		if !ok || t.pointer {
			return false
		}
		if ident.Path != "" {
			// Populated by a resolving decorator; already the real import path.
			return t.importPath == ident.Path && t.name == n.Sel.Name
		}
		if resolved, importOk := imports[ident.Name]; importOk {
			return t.importPath == resolved && t.name == n.Sel.Name
		}
		// No import context at all (imports == nil, e.g. hand-built AST nodes in
		// tests with no backing *dst.File): compare against importPath's last
		// segment. Note this cannot rescue a miskeyed map — a tail match here
		// would imply ident.Name is a key, so the lookup above would have hit.
		return defaultImportAlias(t.importPath) == ident.Name && t.name == n.Sel.Name

	case *dst.StarExpr:
		inner := parsedTypeName{importPath: t.importPath, name: t.name}
		return t.pointer && inner.matches(n.X, imports)

	case *dst.IndexExpr:
		// Generic type with a single type parameter (e.g. Seq[T]).
		return !t.pointer && t.matches(n.X, imports)

	case *dst.IndexListExpr:
		// Generic type with multiple type parameters (e.g. Map[K, V]).
		return !t.pointer && t.matches(n.X, imports)

	case *dst.InterfaceType:
		// Only the empty interface matches "any".
		return len(n.Methods.List) == 0 && t.importPath == "" && t.name == "any"

	default:
		// Unsupported AST node types (chan, func, map, slice, array, interface
		// literals) can never satisfy a plain type-name filter.
		return false
	}
}

// fieldListContainsType reports whether any field in fields has a type that
// matches typeStr.
// Returns an error when typeStr cannot be parsed.
func fieldListContainsType(fields *dst.FieldList, typeStr string, imports map[string]string) (bool, error) {
	if fields == nil || len(fields.List) == 0 {
		return false, nil
	}
	tn, err := parseTypeName(typeStr)
	if err != nil {
		return false, err
	}
	for _, field := range fields.List {
		if tn.matches(field.Type, imports) {
			return true, nil
		}
	}
	return false, nil
}

// MatchesTypeName reports whether node's type matches the type-name string
// typeStr. imports resolves the local identifier used at node's use site to
// its real import path; see ImportAliasMap. Pass nil when no import context
// is available
// Returns an error when typeStr cannot be parsed.
func MatchesTypeName(node dst.Expr, typeStr string, imports map[string]string) (bool, error) {
	tn, err := parseTypeName(typeStr)
	if err != nil {
		return false, err
	}
	return tn.matches(node, imports), nil
}

// importAliasMap builds a map from the local identifier used to reference an
// imported package within file (its explicit alias, or its default package
// name when unaliased) to that package's real import path. It correctly disambiguates:
//   - aliased imports (e.g. `import althttp "net/http"`)
//   - distinct import paths that happen to share a last path segment (e.g.
//     "text/template" vs "html/template", both conventionally "template")
//
// This deliberately duplicates tool/internal/imports.parseFile rather than reusing
// it: that resolves unaliased imports with pkgload.ResolvePackageName (a
// go/packages load that ex.Fatalf's on failure), which is too costly and too fatal
// for the setup/match path, where this runs for every compiled package in the build.
// The cost is that the default name here is a syntactic guess; see defaultImportAlias.
//
// Returns nil when file is nil.
func ImportAliasMap(file *dst.File) map[string]string {
	if file == nil {
		return nil
	}
	var specs []*dst.ImportSpec
	if len(file.Imports) > 0 {
		specs = file.Imports
	} else {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*dst.GenDecl)
			if !ok || genDecl.Tok != token.IMPORT {
				continue
			}
			for _, spec := range genDecl.Specs {
				if importSpec, isImport := spec.(*dst.ImportSpec); isImport {
					specs = append(specs, importSpec)
				}
			}
		}
	}

	aliases := make(map[string]string, len(specs))
	for _, imp := range specs {
		if imp.Path == nil {
			continue
		}
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		alias := defaultImportAlias(path)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		// Blank and dot imports don't introduce a qualified identifier that a
		// type reference could use, so they can't participate in matching.
		if alias == "" || alias == "_" || alias == "." {
			continue
		}
		aliases[alias] = path
	}
	return aliases
}

// defaultImportAlias (also aliased as ImportPathTail) returns the local identifier
// conventionally used to reference an import path: its last segment, ignoring a
// Go module major-version suffix ("/v2".."/vN", or gopkg.in's ".vN"), which is
// part of the module path but not of the package name, e.g. "net/http" -> "http",
// "github.com/x/jwt/v5" -> "jwt", "gopkg.in/yaml.v3" -> "yaml".
//
// This is a convention, not a guarantee: a package may declare a name unrelated
// to its path (e.g. "github.com/redis/go-redis/v9" declares "redis"). Such
// packages are matched only when the importing file aliases them explicitly.
func defaultImportAlias(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "." || path == "/" {
		return ""
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 && isMajorVersion(path[i+1:]) {
		path = path[:i]
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	// gopkg.in style: "yaml.v3" -> "yaml".
	if i := strings.LastIndexByte(path, '.'); i >= 0 && isMajorVersion(path[i+1:]) {
		path = path[:i]
	}
	if path == "." || path == "/" {
		return ""
	}
	return path
}

// isMajorVersion reports whether s is a module major-version element ("v2", "v11").
func isMajorVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, c := range []byte(s[1:]) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
