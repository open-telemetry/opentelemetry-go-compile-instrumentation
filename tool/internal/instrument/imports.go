// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"slices"

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
// The rule's alias is imported even when the file already imports the same
// path under another alias: Go accepts one path imported under several
// aliases, so the injected code compiles as the rule wrote it. A dot import is
// the exception, and the file must dot-import the path itself.
func (ip *instrumentPhase) addRuleImports(
	ctx context.Context,
	root *dst.File,
	ruleImports map[string]string,
	ruleName string,
) error {
	if len(ruleImports) == 0 {
		return nil
	}

	resolution, err := imports.FindNew(ctx, root, ruleImports, ip.importNames)
	if err != nil {
		return ex.Wrapf(err, "resolving imports for %s", ruleName)
	}

	// A dot import needs the file to dot-import the path itself: another
	// alias for the path does not bring its identifiers into scope.
	if importPath, ok := ruleImports["."]; ok {
		if aliases, pathExists := resolution.ExistingAliasSets[importPath]; pathExists &&
			!slices.Contains(aliases, ".") {
			return ex.Newf(
				"%s: dot-import conflict for %q - "+
					"file imports the path with aliases %v but rule requires dot-import; "+
					"injected unqualified identifiers will not resolve; "+
					"either update the file to use dot-import or adjust the rule",
				ruleName, importPath, aliases)
		}
	}

	if len(resolution.NewImports) == 0 {
		return nil
	}

	// Add import declarations to the AST
	if err = imports.AddToFile(ctx, root, resolution.NewImports, ip.importNames); err != nil {
		return ex.Wrapf(err, "adding imports for %s", ruleName)
	}

	// Update importcfg for the build
	if err = ip.updateImportConfig(ctx, resolution.NewImports); err != nil {
		return ex.Wrapf(err, "updating import config for %s", ruleName)
	}

	return nil
}

// importAliasMap returns root's import aliases, each mapped to its import
// path, for the template helpers that resolve a type name.
func (ip *instrumentPhase) importAliasMap(root *dst.File) map[string]string {
	return ast.ImportAliasMap(root, ip.importNames)
}

// injectedRuleAliases returns the aliases of ruleImports that nodes use as the
// qualifier of a selector, such as `alias.Func`. These are the aliases that a
// local identifier at the injection point can shadow. The dot and blank aliases
// are never returned.
func injectedRuleAliases(ruleImports map[string]string, nodes ...dst.Node) map[string]bool {
	aliases := make(map[string]bool)
	if len(ruleImports) == 0 {
		return aliases
	}
	for _, node := range nodes {
		if node == nil {
			continue
		}
		dst.Inspect(node, func(n dst.Node) bool {
			sel, ok := n.(*dst.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*dst.Ident)
			if !ok || ident.Name == "_" || ident.Name == "." {
				return true
			}
			if _, isRuleAlias := ruleImports[ident.Name]; isRuleAlias {
				aliases[ident.Name] = true
			}
			return true
		})
	}
	return aliases
}

// injectedStmtsRuleAliases is injectedRuleAliases for a list of statements.
func injectedStmtsRuleAliases(ruleImports map[string]string, stmts []dst.Stmt) map[string]bool {
	nodes := make([]dst.Node, len(stmts))
	for i, stmt := range stmts {
		nodes[i] = stmt
	}
	return injectedRuleAliases(ruleImports, nodes...)
}

// checkRuleAliasShadowing reports an error when a rule alias names a
// receiver, parameter, named return value, or body-level variable of
// enclosing, all of which shadow the import in code injected at the
// function's top level.
//
// It is checkRuleAliasShadowingAt without an injection point.
//
// checkRuleAliasShadowing reports no error when enclosing is nil.
func checkRuleAliasShadowing(aliases map[string]bool, enclosing *dst.FuncDecl) error {
	return checkRuleAliasShadowingAt(aliases, enclosing, nil)
}

// checkRuleAliasShadowingAt reports an error when a rule alias would resolve
// to a local instead of the import at target's injection point.
//
// The rule's alias is imported at file scope, so any name bound in a scope
// around the injected code shadows it. Code injected at the function's top
// level is guarded by the receiver, parameter, named return, and body-level
// names alone. Rules that inject deeper (call rules at the call site, raw
// rules with a pattern, and literal rules at the literal) can also be
// shadowed by a name bound between the function body and target: a nested
// block, a for/range/if/switch init statement, a case or select clause, or a
// closure parameter.
//
// As with the top-level names, a name bound anywhere within a scope on the
// path is reported even when it is declared after target, which keeps the
// check conservative without relying on source positions.
//
// A nil target checks only the function top level.
//
// checkRuleAliasShadowingAt reports no error when enclosing is nil.
func checkRuleAliasShadowingAt(aliases map[string]bool, enclosing *dst.FuncDecl, target dst.Node) error {
	if len(aliases) == 0 || enclosing == nil {
		return nil
	}
	localNames := signatureNames(enclosing)
	mergeNamesInto(localNames, topLevelBodyNames(enclosing))
	if err := reportRuleAliasConflict(aliases, enclosing.Name.Name, localNames,
		"a parameter, receiver, named return, or body-level variable"); err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	return reportRuleAliasConflict(aliases, enclosing.Name.Name, scopeNamesBetween(enclosing, target),
		"a local declared between the function's top level and the injection point")
}

// checkRuleAliasShadowingInDecl reports an error when a rule alias would
// resolve to a local instead of the import at target, a node in the
// initializer of the package-level declaration decl. The locals are the
// parameters and body names of the function literals that enclose target.
func checkRuleAliasShadowingInDecl(aliases map[string]bool, decl *dst.GenDecl, target dst.Node) error {
	if len(aliases) == 0 {
		return nil
	}
	return reportRuleAliasConflict(aliases, "a package-level declaration", scopeNamesBetween(decl, target),
		"a parameter or local of an enclosing function literal")
}

// reportRuleAliasConflict returns an error when any of aliases names a local
// in localNames. description names what the alias would resolve to.
func reportRuleAliasConflict(
	aliases map[string]bool,
	funcName string,
	localNames map[string]bool,
	description string,
) error {
	names := make([]string, 0, len(aliases))
	for alias := range aliases {
		names = append(names, alias)
	}
	slices.Sort(names)
	for _, alias := range names {
		if localNames[alias] {
			return ex.Newf(
				"rule import alias conflict in %s: the injected code uses the rule alias %q, "+
					"which resolves to %s instead of the import; rename the rule's import "+
					"alias or the local identifier",
				funcName, alias, description)
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
// Blank ("_") and dot (".") aliases are always kept.
func usedRuleImports(root *dst.File, ruleImports map[string]string) map[string]string {
	if len(ruleImports) == 0 {
		return nil
	}

	used := make(map[string]string, len(ruleImports))
	unresolved := make(map[string]string, len(ruleImports))
	for alias, path := range ruleImports {
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
