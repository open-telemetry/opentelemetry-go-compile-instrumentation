// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"slices"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
)

// updateImportConfigForFile ensures all imports in the given file's AST are present in the importcfg.
// This is used when adding a new file (e.g., via file rules) that has its own imports which may
// not be in the target package's importcfg.
func (ip *instrumentPhase) updateImportConfigForFile(ctx context.Context, root *dst.File, ruleName string) error {
	paths := imports.CollectImportPaths(root)

	if len(paths) == 0 {
		return nil
	}

	if err := ip.updateImportConfig(ctx, paths); err != nil {
		return ex.Wrapf(err, "updating import config for file imports in %s", ruleName)
	}

	return nil
}

// addRuleImports processes imports for a rule and updates the import config.
//
// This function validates that if a rule expects to use an import with a specific alias,
// and the file already imports the same package with a different alias (whether explicit or
// implicit), an error is returned. This prevents silent failures where injected code uses
// an alias that doesn't exist in the file.
func (ip *instrumentPhase) addRuleImports(
	ctx context.Context,
	root *dst.File,
	ruleImports map[string]string,
	ruleName string,
) error {
	if len(ruleImports) == 0 {
		return nil
	}

	resolution := imports.FindNew(ctx, root, ruleImports)

	// Validate: check for alias mismatches that would break injected code
	for ruleAlias, importPath := range ruleImports {
		if ruleAlias == "." {
			// Dot-import conflict check: the file must dot-import the path itself
			if aliases, pathExists := resolution.ExistingAliasSets[importPath]; pathExists {
				if !slices.Contains(aliases, ".") {
					return ex.Newf(
						"%s: dot-import conflict for %q - "+
							"file imports the path with aliases %v but rule requires dot-import; "+
							"injected unqualified identifiers will not resolve; "+
							"either update the file to use dot-import or adjust the rule",
						ruleName, importPath, aliases)
				}
			}
			continue
		}
		if ruleAlias == "_" {
			continue // Blank imports are permissive
		}

		// Validate alias matches for all existing imports (both explicit and implicit).
		// When a file already imports a path, we won't add a duplicate, so injected code
		// must use an alias that actually exists in the file. A file may import the
		// path under several aliases; each of them is valid.
		if aliases, pathExists := resolution.ExistingAliasSets[importPath]; pathExists {
			if !slices.Contains(aliases, ruleAlias) {
				return ex.Newf(
					"%s: import alias mismatch for %q - "+
						"file uses aliases %v but rule expects %q; "+
						"injected code will fail to compile; "+
						"either update the file's import or adjust the rule's import alias",
					ruleName, importPath, aliases, ruleAlias)
			}
		}
	}

	if len(resolution.NewImports) == 0 {
		return nil
	}

	// Add import declarations to the AST
	if err := imports.AddToFile(ctx, root, resolution.NewImports); err != nil {
		return ex.Wrapf(err, "adding imports for %s", ruleName)
	}

	// Update importcfg for the build
	if err := ip.updateImportConfig(ctx, resolution.NewImports); err != nil {
		return ex.Wrapf(err, "updating import config for %s", ruleName)
	}

	return nil
}

// resolveImportOverrides computes root's current import aliases and the
// per-rule-alias overrides a rule needs when the file already imports one of
// ruleImports' paths under a different alias.
//
//nolint:revive // needed to balance confusing-results and nonamedreturns linters
func (ip *instrumentPhase) resolveImportOverrides(
	root *dst.File,
	ruleImports map[string]string,
) (map[string]string, map[string]string) {
	importAliases := ast.ImportAliasMap(root, ip.importNames)

	// Collect every alias the file uses for each path, sorted, so alias
	// selection stays the same across every call
	resolvedAliases := ast.ResolvedImportAliasMap(root, ip.importNames)
	existingAliasSets := make(map[string][]string, len(resolvedAliases))
	for alias, path := range resolvedAliases {
		existingAliasSets[path] = append(existingAliasSets[path], alias)
	}
	for _, aliases := range existingAliasSets {
		slices.Sort(aliases)
	}
	aliasOverrides := resolveAliasOverrides(ruleImports, existingAliasSets)
	return importAliases, aliasOverrides
}

// resolveAliasOverrides reports the alias to substitute for each rule
// import the target file already imports under a different alias.
// Substituting the file's alias into generated code, instead of the
// rule's alias, avoids a build failure.
//
// existingAliasSets maps each import path to every alias the file uses
// for it. A rule alias that already names the path in the file needs no
// substitution: the generated code compiles as written.
//
// existingAliasSets must resolve an unaliased import to its real name,
// not a guess. A guessed name can be illegal as a Go identifier.
//
// The dot alias and the blank alias are exempt from substitution.
func resolveAliasOverrides(ruleImports map[string]string, existingAliasSets map[string][]string) map[string]string {
	var overrides map[string]string
	for ruleAlias, importPath := range ruleImports {
		if ruleAlias == "." || ruleAlias == "_" {
			continue
		}
		aliases, ok := existingAliasSets[importPath]
		if !ok || slices.Contains(aliases, ruleAlias) {
			continue
		}
		if overrides == nil {
			overrides = make(map[string]string, len(ruleImports))
		}
		// The smallest alias keeps the substitution deterministic. Dot and
		// blank aliases never reach the set; ast.ImportAliasMap skips them.
		overrides[ruleAlias] = aliases[0]
	}
	return overrides
}

// replaceQualifierAliases rewrites a qualifier the rule wrote to use the
// file's alias for an import, instead of the rule's alias. overrides maps
// each rule alias to the file's alias.
//
// Call this before stripDynamicIdents, while a target-derived identifier
// (an argument, receiver, or function name) still carries dynamicIdentMarker.
// The marker makes its name a different string from any rule alias, so a
// target name that happens to match a rule's alias is never mistaken for a
// qualifier the rule wrote.
func replaceQualifierAliases(node dst.Node, overrides map[string]string) {
	if len(overrides) == 0 {
		return
	}
	dst.Inspect(node, func(n dst.Node) bool {
		sel, ok := n.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		if existingAlias, override := overrides[ident.Name]; override {
			ident.Name = existingAlias
		}
		return true
	})
}

// stripDynamicIdents removes dynamicIdentMarker from every identifier and
// string literal in node, restoring the target's original name. Call this
// once on rendered template output.
// replaceQualifierAliases should always be called before stripDynamicIdents.
func stripDynamicIdents(node dst.Node) {
	dst.Inspect(node, func(n dst.Node) bool {
		switch lit := n.(type) {
		case *dst.Ident:
			lit.Name = strings.TrimPrefix(lit.Name, dynamicIdentMarker)
		case *dst.BasicLit:
			lit.Value = strings.ReplaceAll(lit.Value, dynamicIdentMarker, "")
		}
		return true
	})
}

// checkAliasOverrideShadowing reports an error when an override's file
// alias names a receiver, parameter, named return value, or body-level
// variable of `enclosing`, all of which shadow code injected at the function's
// top level.
//
// It is checkAliasOverrideShadowingAt without an injection point.
//
// checkAliasOverrideShadowing reports no error when enclosing is nil.
func checkAliasOverrideShadowing(overrides map[string]string, enclosing *dst.FuncDecl) error {
	return checkAliasOverrideShadowingAt(overrides, enclosing, nil)
}

// checkAliasOverrideShadowingAt reports an error when an override's file alias
// would resolve to a local instead of the import at target's injection point.
//
// The override substitutes the file's alias for the rule's alias in generated
// code. Code injected at the function's top level is guarded by the receiver,
// parameter, named return, and body-level names alone. Rules that inject
// deeper — call rules at the call site, raw rules with a pattern, and literal
// rules at the literal — can also be shadowed by a name bound between the
// function body and target: a nested block, a for/range/if/switch init
// statement, a case or select clause, or a closure parameter.
//
// As with the top-level names, a name bound anywhere within a scope on the
// path is reported even when it is declared after target, which keeps the
// check conservative without relying on source positions.
//
// A nil target checks only the function top level.
//
// checkAliasOverrideShadowingAt reports no error when enclosing is nil.
func checkAliasOverrideShadowingAt(overrides map[string]string, enclosing *dst.FuncDecl, target dst.Node) error {
	if len(overrides) == 0 || enclosing == nil {
		return nil
	}
	localNames := signatureNames(enclosing)
	mergeNamesInto(localNames, topLevelBodyNames(enclosing))
	if err := reportAliasOverrideConflict(overrides, enclosing.Name.Name, localNames,
		"a parameter, receiver, named return, or body-level variable"); err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	return reportAliasOverrideConflict(overrides, enclosing.Name.Name, scopeNamesBetween(enclosing, target),
		"a local declared between the function's top level and the injection point")
}

// checkAliasOverrideShadowingInDecl reports an error when an override's file
// alias would resolve to a local instead of the import at target, a node in
// the initializer of the package-level declaration decl. The locals are the
// parameters and body names of the function literals that enclose target.
func checkAliasOverrideShadowingInDecl(overrides map[string]string, decl *dst.GenDecl, target dst.Node) error {
	if len(overrides) == 0 {
		return nil
	}
	return reportAliasOverrideConflict(overrides, "a package-level declaration", scopeNamesBetween(decl, target),
		"a parameter or local of an enclosing function literal")
}

// reportAliasOverrideConflict returns an error when any of overrides' file
// aliases names a local in localNames. description names what the alias would
// resolve to.
func reportAliasOverrideConflict(
	overrides map[string]string,
	funcName string,
	localNames map[string]bool,
	description string,
) error {
	for ruleAlias, fileAlias := range overrides {
		if localNames[fileAlias] {
			return ex.Newf(
				"alias override conflict in %s: substituting the file's alias %q for "+
					"rule alias %q would resolve to %s instead of the import; rename the "+
					"local identifier or the file's import alias",
				funcName, fileAlias, ruleAlias, description)
		}
	}
	return nil
}

// scopeNamesBetween returns every name bound by a scope on the path from
// enclosing to target. When enclosing is a function, the path starts at the
// function, so the result includes the signature and top-level body names.
func scopeNamesBetween(enclosing, target dst.Node) map[string]bool {
	names := make(map[string]bool)
	var stack []map[string]bool
	found := false
	dstutil.Apply(enclosing, func(cursor *dstutil.Cursor) bool {
		if found {
			return false
		}
		node := cursor.Node()
		if node == target {
			for _, frame := range stack {
				mergeNamesInto(names, frame)
			}
			found = true
			return false
		}
		if frame, ok := scopeFrame(node); ok {
			stack = append(stack, frame)
		}
		return true
	}, func(cursor *dstutil.Cursor) bool {
		if _, ok := scopeFrame(cursor.Node()); ok {
			stack = stack[:len(stack)-1]
		}
		return true
	})
	if !found {
		panic("scopeNamesBetween: target is not within enclosing node")
	}
	return names
}

// scopeFrame returns the names bound by node for the scopes node introduces,
// and reports whether node introduces a scope on the path from a function body
// to an injection point. The frames cover every construct that binds names
// between a function body and a nested statement: the function itself, blocks,
// case and select clauses, for/range/if/switch init statements, range
// variables, and closures.
func scopeFrame(node dst.Node) (map[string]bool, bool) {
	switch n := node.(type) {
	case *dst.FuncDecl:
		return signatureNames(n), true
	case *dst.FuncLit:
		return funcTypeNames(n.Type), true
	case *dst.BlockStmt:
		return stmtDeclNames(n.List), true
	case *dst.CaseClause:
		return stmtDeclNames(n.Body), true
	case *dst.CommClause:
		names := stmtDeclNames(n.Body)
		mergeNamesInto(names, initDeclNames(n.Comm))
		return names, true
	case *dst.IfStmt:
		return initDeclNames(n.Init), true
	case *dst.ForStmt:
		names := initDeclNames(n.Init)
		mergeNamesInto(names, initDeclNames(n.Post))
		return names, true
	case *dst.SwitchStmt:
		return initDeclNames(n.Init), true
	case *dst.TypeSwitchStmt:
		names := initDeclNames(n.Init)
		mergeNamesInto(names, initDeclNames(n.Assign))
		return names, true
	case *dst.RangeStmt:
		return rangeDeclNames(n), true
	}
	return nil, false
}

// initDeclNames returns the names bound by a scope-introducing init, post, or
// comm statement. It returns an empty map when stmt is nil.
func initDeclNames(stmt dst.Stmt) map[string]bool {
	if stmt == nil {
		return make(map[string]bool)
	}
	return stmtDeclNames([]dst.Stmt{stmt})
}

// rangeDeclNames returns the names a range statement binds in its own scope.
func rangeDeclNames(n *dst.RangeStmt) map[string]bool {
	names := make(map[string]bool)
	if n.Tok != token.DEFINE {
		return names
	}
	for _, keyOrVal := range []dst.Expr{n.Key, n.Value} {
		if ident, ok := keyOrVal.(*dst.Ident); ok {
			addLocalName(names, ident)
		}
	}
	return names
}

// signatureNames returns the names bound by funcDecl's receiver, parameters,
// and named return values.
func signatureNames(funcDecl *dst.FuncDecl) map[string]bool {
	names := fieldListNames(funcDecl.Recv)
	mergeNamesInto(names, funcTypeNames(funcDecl.Type))
	return names
}

// funcTypeNames returns the names bound by a function type's parameters and
// named return values.
func funcTypeNames(funcType *dst.FuncType) map[string]bool {
	names := fieldListNames(funcType.Params)
	mergeNamesInto(names, fieldListNames(funcType.Results))
	return names
}

// fieldListNames returns the names bound by a field list.
func fieldListNames(list *dst.FieldList) map[string]bool {
	names := make(map[string]bool)
	if list == nil {
		return names
	}
	for _, field := range list.List {
		for _, name := range field.Names {
			addLocalName(names, name)
		}
	}
	return names
}

// mergeNamesInto adds every name of src to dst.
func mergeNamesInto(dst, src map[string]bool) {
	for name := range src {
		dst[name] = true
	}
}

// topLevelBodyNames returns the names bound directly by funcDecl's body
// statements: var, const, and type declarations, plus short variable
// declarations. Identifiers bound in nested statements and closures are not
// returned, because they do not shadow code injected at the function's top
// level.
func topLevelBodyNames(funcDecl *dst.FuncDecl) map[string]bool {
	if funcDecl.Body == nil {
		return make(map[string]bool)
	}
	return stmtDeclNames(funcDecl.Body.List)
}

// stmtDeclNames returns the names bound directly by stmts: var, const, and
// type declarations, plus short variable declarations. Identifiers bound in
// nested statements are not returned.
func stmtDeclNames(stmts []dst.Stmt) map[string]bool {
	names := make(map[string]bool)
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *dst.DeclStmt:
			decl, ok := s.Decl.(*dst.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *dst.ValueSpec:
					for _, name := range spec.Names {
						addLocalName(names, name)
					}
				case *dst.TypeSpec:
					addLocalName(names, spec.Name)
				}
			}
		case *dst.AssignStmt:
			if s.Tok != token.DEFINE {
				continue
			}
			for _, lhs := range s.Lhs {
				if ident, ok := lhs.(*dst.Ident); ok {
					addLocalName(names, ident)
				}
			}
		}
	}
	return names
}

// addLocalName records name in names, skipping blank identifiers.
func addLocalName(names map[string]bool, name *dst.Ident) {
	if name != nil && name.Name != "" && name.Name != "_" {
		names[name.Name] = true
	}
}

// usedRuleImports returns the subset of ruleImports whose alias is actually
// referenced somewhere in root. It must be called after the rule's generated
// code has already been spliced into root.
//
// Blank ("_") and dot (".") aliases are always kept. An alias present in
// aliasOverrides is dropped instead, never reaching addRuleImports:
// resolveAliasOverrides only produces an override when the file already
// imports that path under a different alias (see resolveAliasOverrides).
func usedRuleImports(root *dst.File, ruleImports, aliasOverrides map[string]string) map[string]string {
	if len(ruleImports) == 0 {
		return nil
	}

	used := make(map[string]string, len(ruleImports))
	unresolved := make(map[string]string, len(ruleImports))
	for alias, path := range ruleImports {
		if _, overridden := aliasOverrides[alias]; overridden {
			continue
		}
		if alias == "_" || alias == "." {
			used[alias] = path
			continue
		}
		unresolved[alias] = path
	}

	if len(unresolved) == 0 {
		return used
	}

	dst.Inspect(root, func(node dst.Node) bool {
		sel, ok := node.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, identOk := sel.X.(*dst.Ident)
		if !identOk {
			return true
		}
		if path, importOk := unresolved[ident.Name]; importOk {
			used[ident.Name] = path
		}
		return true
	})

	return used
}
