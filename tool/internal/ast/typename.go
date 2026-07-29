// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
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
		// No import context resolved this qualifier (nil/incomplete imports
		// map): fall back to comparing against importPath's last segment
		return importPathTail(t.importPath) == ident.Name && t.name == n.Sel.Name

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
		// literals) cannot be matched by type-name filters.
		util.Unimplemented(fmt.Sprintf("signature filter: unsupported type node %T", node))
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

// importAliasMap builds a map from the local identifier used to reference an
// imported package within file (its explicit alias, or its default package
// name when unaliased) to that package's real import path. It correctly disambiguates:
//   - aliased imports (e.g. `import althttp "net/http"`)
//   - distinct import paths that happen to share a last path segment (e.g.
//     "text/template" vs "html/template", both conventionally "template")
//
// Returns nil when file is nil.
func importAliasMap(file *dst.File) map[string]string {
	if file == nil {
		return nil
	}
	aliases := make(map[string]string, len(file.Imports))
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		alias := importPathTail(path)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		// Blank and dot imports don't introduce a qualified identifier that a
		// type reference could use, so they can't participate in matching.
		if alias == "_" || alias == "." {
			continue
		}
		aliases[alias] = path
	}
	return aliases
}

// importPathTail returns the last path segment of an import path, which is
// conventionally (but not necessarily) that package's default identifier,
// e.g. "net/http" -> "http".
func importPathTail(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
