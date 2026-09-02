// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"slices"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// applyLitRule sets fields on every composite literal of the rule's type found
// in the target file.
func (ip *instrumentPhase) applyLitRule(ctx context.Context, r *rule.InstLitRule, root *dst.File) error {
	importAliases := ast.ImportAliasMap(root)

	setters, err := newLitFieldSetters(r)
	if err != nil {
		return err
	}

	var matched []*dst.CompositeLit
	dst.Inspect(root, func(node dst.Node) bool {
		lit, ok := node.(*dst.CompositeLit)
		if !ok {
			return true
		}
		if matchesLitRule(lit, r, importAliases) {
			matched = append(matched, lit)
		}
		return true
	})

	modified := false
	// Walk matched in reverse. dst.Inspect visits a literal before the literals
	// nested inside it, so the reverse order edits the innermost literal first
	// and an enclosing literal always wraps a finished subtree.
	for _, lit := range slices.Backward(matched) {
		// Go rejects a literal that mixes keyed and positional elements, so a
		// positional literal cannot take the keyed elements this rule produces.
		if hasPositionalElements(lit) {
			ip.Warn("Skipping positional composite literal; set_fields requires keyed elements",
				"rule", r.Name, "type", r.StructLiteral)
			continue
		}
		litModified, setErr := ip.setLitFields(lit, setters, r)
		if setErr != nil {
			return setErr
		}
		modified = modified || litModified
	}

	if !modified {
		return nil
	}

	if err = ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}
	ip.Info("Apply literal rule", "rule", r)

	return nil
}

// litFieldSetter holds a field's parsed instructions, built once per rule.
// Applying wrap still happens per literal, since it substitutes that literal's
// own expression.
type litFieldSetter struct {
	name string
	// value is the parsed expression for a literal that omits the field. It is
	// nil when the rule only set wrap, which leaves such literals alone.
	value dst.Expr
	// wrap is the compiled template applied to a value the literal already
	// assigns. It is nil when the rule only set value.
	wrap *callTemplate
}

func newLitFieldSetters(r *rule.InstLitRule) ([]*litFieldSetter, error) {
	setters := make([]*litFieldSetter, 0, len(r.Field))
	for _, f := range r.Field {
		setter := &litFieldSetter{name: f.Name}
		if f.Value != "" {
			value, err := parseGoExpression(f.Value)
			if err != nil {
				return nil, ex.Wrapf(err, "failed to parse value %q for field %q", f.Value, f.Name)
			}
			setter.value = value
		}
		if f.Wrap != "" {
			tmpl, err := newCallTemplate(f.Wrap)
			if err != nil {
				return nil, ex.Wrapf(err, "failed to compile wrap template for field %q", f.Name)
			}
			setter.wrap = tmpl
		}
		setters = append(setters, setter)
	}
	return setters, nil
}

// matchesLitRule reports whether a composite literal builds the rule's type.
//
// Only qualified literals are matched: pkg.Type{...}, including the &pkg.Type{...}
// form, whose composite literal node is the same. A literal with an elided type
// (an element of a slice or map literal) has no type to match and is skipped.
func matchesLitRule(lit *dst.CompositeLit, r *rule.InstLitRule, importAliases map[string]string) bool {
	sel, ok := lit.Type.(*dst.SelectorExpr)
	if !ok {
		return false
	}

	if sel.Sel.Name != r.TypeName {
		return false
	}

	// The package qualifier must be a plain identifier, not a chained selector.
	ident, ok := sel.X.(*dst.Ident)
	if !ok {
		return false
	}

	pkgPath := ident.Path
	if pkgPath != "" {
		return pkgPath == r.ImportPath
	}

	resolvedPath, ok := importAliases[ident.Name]
	return ok && resolvedPath == r.ImportPath
}

// hasPositionalElements reports whether the literal uses positional elements.
// An empty literal has none and can take keyed elements.
func hasPositionalElements(lit *dst.CompositeLit) bool {
	for _, elt := range lit.Elts {
		if _, ok := elt.(*dst.KeyValueExpr); !ok {
			return true
		}
	}
	return false
}

// setLitFields applies each setter to the literal, overriding fields already
// present in place and prepending the rest. The literal's own elements are
// otherwise left untouched. It reports whether the literal changed.
func (ip *instrumentPhase) setLitFields(
	lit *dst.CompositeLit,
	setters []*litFieldSetter,
	r *rule.InstLitRule,
) (bool, error) {
	changed := false
	var prepend []dst.Expr
	for _, setter := range setters {
		util.Assert(setter.value != nil || setter.wrap != nil, "field setter has neither value nor wrap")

		if existing := findKeyedElement(lit, setter.name); existing != nil {
			if setter.wrap == nil {
				existing.Value = util.AssertType[dst.Expr](dst.Clone(setter.value))
				changed = true
				continue
			}
			// Composite literal fields have no enclosing function context.
			wrapped, err := setter.wrap.compileExpression(existing.Value, nil)
			if err != nil {
				return false, ex.Wrapf(err, "failed to wrap field %q of %s", setter.name, r.StructLiteral)
			}
			// wrapped holds the literal's own expression, so cloning it would
			// detach any literal nested in that expression from the tree.
			// compileExpression already returns a fresh tree, and the old value
			// leaves the tree in the same assignment, so no node is reused.
			existing.Value = wrapped
			changed = true
			continue
		}

		// Nothing for wrap to substitute when the literal omits the field.
		if setter.value == nil {
			ip.Warn("Skipping field absent from composite literal; wrap has no value to substitute",
				"rule", r.Name, "type", r.StructLiteral, "field", setter.name)
			continue
		}
		prepend = append(prepend, &dst.KeyValueExpr{
			Key:   &dst.Ident{Name: setter.name},
			Value: util.AssertType[dst.Expr](dst.Clone(setter.value)),
		})
	}
	if len(prepend) > 0 {
		lit.Elts = append(prepend, lit.Elts...)
		changed = true
	}
	return changed, nil
}

// findKeyedElement returns the literal's element keyed by name, or nil.
func findKeyedElement(lit *dst.CompositeLit, name string) *dst.KeyValueExpr {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := kv.Key.(*dst.Ident)
		if !ok || ident.Name != name {
			continue
		}
		return kv
	}
	return nil
}
