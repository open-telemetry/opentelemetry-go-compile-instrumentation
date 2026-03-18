// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"path"
	"strings"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

// matchesCallRule checks if a call expression matches the rule's criteria.
//
// Only qualified calls are supported: pkg.Function()
// The function_call rule must specify the full import path: "package/path.FunctionName"
//
// Examples in source code:
//   - http.Get() after "import 'net/http'" matches "net/http.Get"
//   - redis.Get() after "import redis 'github.com/redis/go-redis/v9'" matches "github.com/redis/go-redis/v9.Get"
//   - sql.Open() after "import 'database/sql'" matches "database/sql.Open"
//
// What does NOT match:
//   - Get() without package qualifier (unqualified calls not supported)
//   - other.Get() where other is from a different package
func matchesCallRule(call *dst.CallExpr, r *rule.InstCallRule, importAliases map[string]string) bool {
	return matchesQualifiedSelector(call.Fun, r.ImportPath, r.FuncName, importAliases)
}

// matchesQualifiedSelector reports whether expr is a selector expression (pkg.Name)
// where Name matches the given name and the package resolves to importPath.
// Resolution uses ident.Path first, then the imports alias map.
func matchesQualifiedSelector(expr dst.Expr, importPath, name string, imports map[string]string) bool {
	sel, ok := expr.(*dst.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != name {
		return false
	}
	ident, ok := sel.X.(*dst.Ident)
	if !ok {
		return false
	}
	// dst may populate Path directly on the identifier when the import is known.
	if ident.Path != "" {
		return ident.Path == importPath
	}
	resolvedPath, ok := imports[ident.Name]
	return ok && resolvedPath == importPath
}

func collectImportAliases(file *dst.File) map[string]string {
	aliases := make(map[string]string)
	for _, decl := range file.Decls {
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
			} else {
				alias = defaultImportAlias(importPath)
			}
			if alias == "" || alias == "_" || alias == "." {
				continue
			}
			aliases[alias] = importPath
		}
	}
	return aliases
}

// defaultImportAlias infers the package alias from an import path, replicating
// Go's default alias rules (last path element, versioned-path stripping, gopkg.in
// conventions). This is a best-effort reimplementation — edge cases like package
// names containing hyphens, _test packages, or build-tag-conditional imports may
// behave differently from the Go compiler's own resolution.
func defaultImportAlias(importPath string) string {
	base := path.Base(importPath)
	if base == "." || base == "/" {
		return ""
	}
	if strings.HasPrefix(importPath, "gopkg.in/") {
		if trimmed := trimGopkgInVersion(base); trimmed != "" {
			return trimmed
		}
	}
	if isVersionSuffix(base) {
		parent := path.Base(path.Dir(importPath))
		if parent != "." && parent != "/" {
			return parent
		}
	}
	return base
}

func trimGopkgInVersion(base string) string {
	idx := strings.LastIndex(base, ".v")
	if idx <= 0 {
		return ""
	}
	if !isDigits(base[idx+2:]) {
		return ""
	}
	return base[:idx]
}

func isVersionSuffix(base string) bool {
	if len(base) < 2 || base[0] != 'v' {
		return false
	}
	return isDigits(base[1:])
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
