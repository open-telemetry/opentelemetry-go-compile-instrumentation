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
// The function-call rule must specify the full import path: "package/path.FunctionName"
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
	// Use pre-parsed fields - no parsing needed!
	importPath := r.ImportPath
	funcName := r.FuncName

	// Only match qualified calls: pkg.Function()
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return false
	}

	// Check function name matches
	if sel.Sel.Name != funcName {
		return false
	}

	// Check that the package identifier is a simple identifier (not a chained selector)
	ident, ok := sel.X.(*dst.Ident)
	if !ok {
		return false
	}

	// Check that the package's import path matches the rule's import path.
	pkgPath := ident.Path
	if pkgPath != "" {
		return pkgPath == importPath
	}

	resolvedPath, ok := importAliases[ident.Name]
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
