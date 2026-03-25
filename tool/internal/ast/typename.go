// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/dave/dst"
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
		return parsedTypeName{}, fmt.Errorf("invalid type name %q", s)
	}
	return parsedTypeName{pointer: m[1] == "*", importPath: m[2], name: m[3]}, nil
}

// matches reports whether the dst.Expr node represents this type.
func (t parsedTypeName) matches(node dst.Expr) bool {
	switch n := node.(type) {
	case *dst.Ident:
		return !t.pointer && t.importPath == n.Path && t.name == n.Name

	case *dst.SelectorExpr:
		ident, ok := n.X.(*dst.Ident)
		if !ok || ident.Path != "" {
			return false
		}
		return !t.pointer && t.importPath == ident.Name && t.name == n.Sel.Name

	case *dst.StarExpr:
		inner := parsedTypeName{importPath: t.importPath, name: t.name}
		return t.pointer && inner.matches(n.X)

	case *dst.IndexExpr:
		// Generic type with a single type parameter (e.g. Seq[T]).
		return !t.pointer && t.matches(n.X)

	case *dst.IndexListExpr:
		// Generic type with multiple type parameters (e.g. Map[K, V]).
		return !t.pointer && t.matches(n.X)

	case *dst.InterfaceType:
		// Only the empty interface matches "any".
		return len(n.Methods.List) == 0 && t.importPath == "" && t.name == "any"

	default:
		// Unsupported AST node types (chan, func, map, slice, array, interface
		// literals) cannot be matched by type-name filters.  Log a warning so
		// the user knows the filter will never fire for this parameter/result.
		slog.Warn("signature filter: unsupported type node; filter will not match",
			"node_type", fmt.Sprintf("%T", node),
			"filter", t.importPath+"."+t.name,
		)
		return false
	}
}

// fieldListContainsType reports whether any field in fields has a type that
// matches typeStr.  Returns false when typeStr is invalid or fields is empty.
func fieldListContainsType(fields *dst.FieldList, typeStr string) bool {
	if fields == nil || len(fields.List) == 0 {
		return false
	}
	tn, err := parseTypeName(typeStr)
	if err != nil {
		return false
	}
	for _, field := range fields.List {
		if tn.matches(field.Type) {
			return true
		}
	}
	return false
}
