// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/dave/dst/dstutil"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	unnamedRetValName = "_unnamedRetVal"
	ignoredParam      = "_ignoredParam"
	// Blank named returns get their own prefix; sharing ignoredParam with a blank
	// param or receiver would collide, since both live in the same scope.
	ignoredRetValName = "_ignoredRetVal"
)

// renameReturnValues assigns referenceable names to funcDecl's unnamed return
// values. Raw and directive rules reference these by the resulting bare
// "_unnamedRetValN" name in their injected code (e.g. the runtime
// instrumentation's goroutine_propagate rule), so the naming here is a
// stable, positional convention and intentionally not salted with a rule
// hash the way collectReturnValues/collectArguments are for func rules.
func renameReturnValues(funcDecl *dst.FuncDecl) {
	if retList := funcDecl.Type.Results; retList != nil {
		idx := 0
		for _, field := range retList.List {
			if field.Names == nil {
				name := fmt.Sprintf("%s%d", unnamedRetValName, idx)
				field.Names = []*dst.Ident{ast.Ident(name)}
				idx++
			}
		}
	}
}

// renderRawCode renders the shared function template variables (FuncName,
// FuncArgument N, FuncReturn N, ...) in raw code injected by a raw rule. Raw
// code that does not contain "{{" is returned unchanged. hash salts synthetic
// argument/return names the same way InstRawRule.Identity salts other rules'
// trampoline/template names.
//
// importAliases maps a use site's local identifier to its real import
// path, for {{.FuncArgumentOfType}} and {{.FuncReturnOfType}}. Passing
// nil forces those template functions to guess the package name, even
// when the real name is known.
func renderRawCode(raw string, decl *dst.FuncDecl, importAliases map[string]string, hash string) (string, error) {
	if !strings.Contains(raw, "{{") {
		return raw, nil
	}
	tmpl, err := rule.ParseFuncTemplate(raw)
	if err != nil {
		return "", err
	}
	return tmpl.Execute(renderingFuncTemplateData{newFuncTemplateData(decl, nil, importAliases, hash)})
}

type insertPos struct {
	pattern   *regexp.Regexp
	placement string
}

// rawInsertion bundles the statements to insert, the position to insert them
// at, and the alias overrides guarding that position, keeping
// insertRawAtPattern's parameter count within the linter's limit.
type rawInsertion struct {
	pos            insertPos
	stmts          []dst.Stmt
	aliasOverrides map[string]string
}

func insertRawAtPattern(
	ctx context.Context,
	decl *dst.FuncDecl,
	restorer *decorator.Restorer,
	insertion rawInsertion,
) (bool, error) {
	pos, stmts, aliasOverrides := insertion.pos, insertion.stmts, insertion.aliasOverrides
	inserted := false
	var shadowErr error
	logger := util.LoggerFromContext(ctx)

	dstutil.Apply(decl.Body, func(cursor *dstutil.Cursor) bool {
		if inserted || shadowErr != nil {
			return false
		}

		stmt, isStmt := cursor.Node().(dst.Stmt)
		if !isStmt {
			return true
		}

		if _, ok := cursor.Parent().(*dst.BlockStmt); !ok {
			return true
		}

		text, err := ast.RenderNode(restorer, stmt)
		if err != nil {
			logger.WarnContext(ctx, "Failed to restore AST node to source code", "error", err)
			return true
		}

		logger.DebugContext(ctx, "Matching statement with pattern", "stmt", text, "pattern", pos.pattern.String())
		if !pos.pattern.MatchString(text) {
			return true
		}

		// The raw code is injected at stmt, so a local bound between the
		// function body and stmt would shadow the rewritten qualifiers there.
		if shadowErr = checkAliasOverrideShadowingAt(aliasOverrides, decl, stmt); shadowErr != nil {
			return false
		}

		switch pos.placement {
		default: // default to "before"
			for _, s := range stmts {
				cursor.InsertBefore(s)
			}
		case "after":
			for _, s := range slices.Backward(stmts) {
				cursor.InsertAfter(s)
			}
		}

		inserted = true
		return false
	}, nil)

	return inserted, shadowErr
}

// rawAliasContext bundles the file's import aliases and the rule-import
// overrides insertRaw needs, keeping insertRaw's parameter count within the
// linter's limit.
type rawAliasContext struct {
	imports   map[string]string
	overrides map[string]string
}

func insertRaw(
	ctx context.Context,
	r *rule.InstRawRule,
	decl *dst.FuncDecl,
	root *dst.File,
	aliases rawAliasContext,
) error {
	util.Assert(decl.Name.Name == r.Func, "sanity check")
	util.Assert(decl.Body != nil, "function must have a body")

	// Rename the unnamed return values so that the raw code can reference them
	renameReturnValues(decl)

	raw, err := renderRawCode(r.Raw, decl, aliases.imports, r.Identity())
	if err != nil {
		return ex.Wrapf(err, "rendering template for func %s", decl.Name.Name)
	}

	// Parse the raw code into AST statements
	p := ast.NewAstParser()
	stmts, err := p.ParseSnippet(raw)
	if err != nil {
		return err
	}

	for _, stmt := range stmts {
		replaceQualifierAliases(stmt, aliases.overrides)
		stripDynamicIdents(stmt)
	}

	// if specified, insert raw code at the position matched by the regex
	if r.Pattern != "" {
		restorer := decorator.NewRestorer()
		if _, restoreErr := restorer.RestoreFile(root); restoreErr != nil {
			return ex.Wrapf(restoreErr, "failed to restore the AST")
		}

		pattern, compileErr := regexp.Compile(r.Pattern)
		if compileErr != nil {
			return ex.Wrapf(compileErr, "invalid raw rule pattern %q", r.Pattern)
		}
		pos := insertPos{
			pattern:   pattern,
			placement: r.Placement,
		}

		inserted, insertErr := insertRawAtPattern(ctx, decl, restorer, rawInsertion{
			pos:            pos,
			stmts:          stmts,
			aliasOverrides: aliases.overrides,
		})
		if insertErr != nil {
			return insertErr
		}
		if !inserted {
			return ex.Newf("no statement matches the pattern %s", r.Pattern)
		}

		return nil
	}

	// Insert the raw code into target function body
	decl.Body.List = append(stmts, decl.Body.List...)
	return nil
}

// applyRawRule injects the raw code into the target function at the beginning
// of the function.
func (ip *instrumentPhase) applyRawRule(ctx context.Context, rule *rule.InstRawRule, root *dst.File) error {
	// Find the target function to be instrumented
	funcDecl, ok, err := ast.FindFuncDecl(root, rule, ip.importNames)
	if err != nil {
		return err
	}
	if !ok {
		return ex.Newf("can not find function %s", rule.Func)
	}

	// De-duplicate injection for the same application on this *dst.FuncDecl.
	// Identity() omits Pattern, Placement, and Imports (those do not change
	// template names), so the key includes them. The map is keyed by the
	// target function so a package with several func init() declarations
	// still injects into each one.
	key := rawRuleDedupKey(rule)
	if seen := ip.appliedRawIdentities[funcDecl]; seen != nil {
		if _, dup := seen[key]; dup {
			ip.Debug("Skipping duplicate raw rule", "rule", rule.Name, "func", rule.Func)
			return nil
		}
	}

	importAliases, aliasOverrides := ip.resolveImportOverrides(root, rule.Imports)
	if err = checkAliasOverrideShadowing(aliasOverrides, funcDecl); err != nil {
		return err
	}

	aliases := rawAliasContext{imports: importAliases, overrides: aliasOverrides}
	if err = insertRaw(ctx, rule, funcDecl, root, aliases); err != nil {
		return err
	}

	// Handle imports if specified in the rule.
	if err = ip.addRuleImports(ctx, root, usedRuleImports(root, rule.Imports, aliasOverrides), rule.Name); err != nil {
		return err
	}
	if ip.appliedRawIdentities == nil {
		ip.appliedRawIdentities = make(map[*dst.FuncDecl]map[string]struct{})
	}
	if ip.appliedRawIdentities[funcDecl] == nil {
		ip.appliedRawIdentities[funcDecl] = make(map[string]struct{})
	}
	ip.appliedRawIdentities[funcDecl][key] = struct{}{}
	ip.Info("Apply raw rule", "rule", rule)
	return nil
}

// rawRuleDedupKey identifies one inject_code application. Identity() covers
// target, version, func, recv, and raw; Pattern, Placement, and Imports are
// appended so the same snippet can be applied at different match sites.
func rawRuleDedupKey(r *rule.InstRawRule) string {
	placement := r.Placement
	if placement == "" {
		placement = "before"
	}
	return enc(r.Identity()) + enc(r.Pattern) + enc(placement) + enc(rawRuleImportsKey(r.Imports))
}

func rawRuleImportsKey(imports map[string]string) string {
	if len(imports) == 0 {
		return ""
	}
	keys := make([]string, 0, len(imports))
	for k := range imports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+len(keys))
	for _, k := range keys {
		parts = append(parts, enc(k), enc(imports[k]))
	}
	return strings.Join(parts, "")
}

func enc(s string) string {
	return strconv.Itoa(len(s)) + ":" + s
}
