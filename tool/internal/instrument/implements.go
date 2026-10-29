// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/types"
	"log/slog"
	"slices"
	"strings"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// Names of the template helpers that need type information.
const (
	argumentThatImplements = "FuncArgumentThatImplements"
	returnThatImplements   = "FuncReturnThatImplements"
)

// funcTypes gives the go/types view of the functions in the file being
// instrumented. Without type information (a nil *funcTypes, or a nil info),
// the *_implements selectors and the ThatImplements helpers match the type
// name only, like param and FuncArgumentOfType.
type funcTypes struct {
	info   *methodCallPackageInfo
	parser *ast.AstParser
	logger *slog.Logger
}

// fileTypes returns the type information for the file that ip parsed last.
func (ip *instrumentPhase) fileTypes() *funcTypes {
	return &funcTypes{info: ip.methodCallInfo, parser: ip.parser, logger: ip.logger}
}

// signature returns the type of decl, or nil when it is unknown.
func (ft *funcTypes) signature(decl *dst.FuncDecl) *types.Signature {
	if ft == nil || ft.info == nil || ft.parser == nil {
		return nil
	}
	pos := ft.parser.FindPosition(decl.Name)
	fn, found := ft.info.funcByPos[methodCallPosition{file: pos.Filename, line: pos.Line, col: pos.Column}]
	if !found {
		if ft.logger != nil {
			args := []any{"func", decl.Name.Name}
			if ft.info.firstTypeError != nil {
				args = append(args, "first_type_error", ft.info.firstTypeError)
			}
			ft.logger.Debug("No type information for the function, matching the type name only", args...)
		}
		return nil
	}
	sig, _ := fn.Type().(*types.Signature)
	return sig
}

// lookupInterface resolves name, of the form [pkg.]Name, to an interface. It
// returns nil when the type cannot be found: there is no type information, or
// the package of the interface is not a dependency of the package being
// compiled. A type that is not an interface is an error.
//
//nolint:nilnil // a nil interface means the type was not found
func (ft *funcTypes) lookupInterface(name string) (*types.Interface, error) {
	path, typeName, err := ast.ParseInterfaceName(name)
	if err != nil {
		return nil, err
	}
	var obj types.Object
	if path == "" {
		obj = types.Universe.Lookup(typeName)
	}
	if obj == nil {
		if pkg := ft.lookupPackage(path); pkg != nil {
			obj = pkg.Scope().Lookup(typeName)
		}
	}
	if obj == nil {
		return nil, nil
	}
	if _, isTypeName := obj.(*types.TypeName); !isTypeName {
		return nil, ex.Newf("%q is not a type", name)
	}
	iface, isInterface := obj.Type().Underlying().(*types.Interface)
	if !isInterface {
		return nil, ex.Newf("%q is not an interface type", name)
	}
	return iface, nil
}

// lookupPackage returns the type-checked package with the import path, the
// package being compiled for an empty path, or nil.
func (ft *funcTypes) lookupPackage(path string) *types.Package {
	if ft == nil || ft.info == nil || ft.info.pkg == nil {
		return nil
	}
	if path == "" || path == ft.info.pkg.Path() {
		return ft.info.pkg
	}
	if pkg, found := ft.info.importer.packages[path]; found {
		return pkg
	}
	if mapped, found := ft.info.importer.importMap[path]; found {
		return ft.info.importer.packages[mapped]
	}
	return nil
}

// typedSlot is one parameter or result: its type expression in the source,
// and its go/types type, nil when unknown.
type typedSlot struct {
	expr dst.Expr
	typ  types.Type
}

// typedSlots returns one slot per name in fields, or per field for unnamed
// ones. vars are the go/types variables of the same list, or nil. They are
// not used when their number is different.
func typedSlots(fields *dst.FieldList, vars *types.Tuple) []typedSlot {
	if fields == nil {
		return nil
	}
	var slots []typedSlot
	for _, field := range fields.List {
		for range max(1, len(field.Names)) {
			slots = append(slots, typedSlot{expr: field.Type})
		}
	}
	if vars != nil && vars.Len() == len(slots) {
		for i := range slots {
			slots[i].typ = vars.At(i).Type()
		}
	}
	return slots
}

// firstImplementing returns the index of the first slot whose type is the
// interface name or implements it, or -1. A slot without a valid type, or any
// slot when the interface is not found, matches by type name.
func (ft *funcTypes) firstImplementing(slots []typedSlot, name string, imports map[string]string) (int, error) {
	iface, err := ft.lookupInterface(name)
	if err != nil {
		return -1, err
	}
	for i, slot := range slots {
		if iface != nil && isValidType(slot.typ) {
			if types.Implements(slot.typ, iface) {
				return i, nil
			}
			continue
		}
		matched, matchErr := ast.MatchesTypeName(slot.expr, name, imports)
		if matchErr != nil {
			return -1, matchErr
		}
		if matched {
			return i, nil
		}
	}
	return -1, nil
}

// isValidType reports whether t is known and has no type error.
func isValidType(t types.Type) bool {
	if t == nil {
		return false
	}
	basic, isBasic := t.Underlying().(*types.Basic)
	return !isBasic || basic.Kind() != types.Invalid
}

// matchesImplementsFilters reports whether decl satisfies the *_implements
// sub-filters of r. The setup phase cannot check them, because it has no type
// information.
func (ip *instrumentPhase) matchesImplementsFilters(
	decl *dst.FuncDecl,
	r *rule.InstFuncRule,
	root *dst.File,
) (bool, error) {
	ft := ip.fileTypes()
	var params, results *types.Tuple
	if sig := ft.signature(decl); sig != nil {
		params, results = sig.Params(), sig.Results()
	}
	resultSlots := typedSlots(decl.Type.Results, results)
	type check struct {
		name  string
		slots []typedSlot
	}
	checks := []check{
		{r.ParamImplements, typedSlots(decl.Type.Params, params)},
		{r.ResultImplements, resultSlots},
	}
	if r.LastResultImplements != "" {
		if len(resultSlots) == 0 {
			return false, nil
		}
		checks = append(checks, check{r.LastResultImplements, resultSlots[len(resultSlots)-1:]})
	}

	imports := ast.ImportAliasMap(root, ip.importNames)
	for _, c := range checks {
		if c.name == "" {
			continue
		}
		idx, err := ft.firstImplementing(c.slots, c.name, imports)
		if err != nil || idx < 0 {
			return false, err
		}
	}
	return true, nil
}

// skipFuncRuleForImplements reports whether fr must be skipped because the
// function it found does not satisfy its *_implements sub-filters.
func (ip *instrumentPhase) skipFuncRuleForImplements(
	fr *rule.InstFuncRule,
	decl *dst.FuncDecl,
	found bool,
	root *dst.File,
) (bool, error) {
	if !found || !fr.HasImplementsFilters() {
		return false, nil
	}
	matched, err := ip.matchesImplementsFilters(decl, fr, root)
	if err != nil || matched {
		return false, err
	}
	ip.Debug("Skip func rule, the function does not match its *_implements selectors",
		"rule", fr.Name, "func", fr.Func)
	return true, nil
}

// rsetUsesImplements reports whether a rule in rset uses a *_implements
// selector or a ThatImplements template helper.
func rsetUsesImplements(rset *rule.InstRuleSet) bool {
	return anyRule(rset.FuncRules, (*rule.InstFuncRule).HasImplementsFilters) ||
		anyRule(rset.RawRules, func(r *rule.InstRawRule) bool { return usesImplementsHelper(r.Raw) }) ||
		anyRule(
			rset.DirectiveRules,
			func(r *rule.InstDirectiveRule) bool { return usesImplementsHelper(r.Template) },
		) ||
		anyRule(rset.CallRules, func(r *rule.InstCallRule) bool { return usesImplementsHelper(r.Replace) })
}

func anyRule[T any](rules map[string][]T, pred func(T) bool) bool {
	for _, list := range rules {
		if slices.ContainsFunc(list, pred) {
			return true
		}
	}
	return false
}

// usesImplementsHelper reports whether a template names a ThatImplements
// helper.
func usesImplementsHelper(text string) bool {
	return strings.Contains(text, argumentThatImplements) || strings.Contains(text, returnThatImplements)
}
