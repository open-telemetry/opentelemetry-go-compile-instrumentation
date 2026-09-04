// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// typeDeclFile builds a *dst.File whose single grouped `type (...)` declaration
// holds the given specs, in order.
func typeDeclFile(specs ...dst.Spec) *dst.File {
	return &dst.File{
		Name:  &dst.Ident{Name: "main"},
		Decls: []dst.Decl{&dst.GenDecl{Tok: token.TYPE, Specs: specs}},
	}
}

func TestApplyStructRule_NonStructType_ReturnsError(t *testing.T) {
	// A struct rule whose target names a non-struct type (here an interface) must
	// return a descriptive error, not fatally exit on the *dst.StructType assertion.
	file := typeDeclFile(
		&dst.TypeSpec{Name: &dst.Ident{Name: "Foo"}, Type: &dst.InterfaceType{Methods: &dst.FieldList{}}},
	)
	r := &rule.InstStructRule{
		InstBaseRule: rule.InstBaseRule{Name: "add_field"},
		Struct:       "Foo",
		NewField:     []*rule.InstStructField{{Name: "Traced", Type: "bool"}},
	}

	err := newTestPhase().applyStructRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `can not find struct "Foo"`)
	assert.Contains(t, err.Error(), "not a struct type")
}

func TestApplyStructRule_GroupedTypeBlock_TargetsNamedStruct(t *testing.T) {
	// In a grouped type block the field must land in the NAMED struct, not the
	// first spec (here a non-struct that used to be asserted via Specs[0]).
	target := &dst.StructType{Fields: &dst.FieldList{}}
	file := typeDeclFile(
		&dst.TypeSpec{Name: &dst.Ident{Name: "First"}, Type: &dst.Ident{Name: "int"}},
		&dst.TypeSpec{Name: &dst.Ident{Name: "Second"}, Type: target},
	)
	r := &rule.InstStructRule{
		InstBaseRule: rule.InstBaseRule{Name: "add_field"},
		Struct:       "Second",
		NewField:     []*rule.InstStructField{{Name: "X", Type: "int"}},
	}

	err := newTestPhase().applyStructRule(context.Background(), r, file)

	require.NoError(t, err)
	require.Len(t, target.Fields.List, 1)
	assert.Equal(t, "X", target.Fields.List[0].Names[0].Name)
}

func TestApplyStructRule_QualifiedFieldType(t *testing.T) {
	// A field type referencing a package (e.g. "context.Context") must be
	// added as a real qualified selector, not spliced in as raw text.
	root := parseFile(t, `package main

import "context"

type S struct{}
`)
	r := &rule.InstStructRule{
		InstBaseRule: rule.InstBaseRule{Name: "add_field"},
		Struct:       "S",
		NewField:     []*rule.InstStructField{{Name: "Ctx", Type: "context.Context"}},
	}

	err := newTestPhase().applyStructRule(context.Background(), r, root)

	require.NoError(t, err)
	structType := findStructTypeInFile(t, root, "S")
	require.Len(t, structType.Fields.List, 1)
	field := structType.Fields.List[0]
	assert.Equal(t, "Ctx", field.Names[0].Name)
	sel, ok := field.Type.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", field.Type)
	ident, ok := sel.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "context", ident.Name)
	assert.Equal(t, "Context", sel.Sel.Name)
}

func TestApplyStructRule_InvalidFieldType(t *testing.T) {
	file := typeDeclFile(
		&dst.TypeSpec{Name: &dst.Ident{Name: "S"}, Type: &dst.StructType{Fields: &dst.FieldList{}}},
	)
	r := &rule.InstStructRule{
		InstBaseRule: rule.InstBaseRule{Name: "add_field"},
		Struct:       "S",
		NewField:     []*rule.InstStructField{{Name: "Bad", Type: "func("}},
	}

	err := newTestPhase().applyStructRule(context.Background(), r, file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to parse type "func("`)
}

// --- import alias override tests ---

func TestApplyStructRule_ImportAliasMismatchUsesFileExistingAlias(t *testing.T) {
	// The rule's field type is written against the alias "context" for
	// "context". The file already imports "context" under its own alias
	// "stdctx". The injected field type must use "stdctx", not fail the
	// build.
	root := parseFile(t, `package main

import stdctx "context"

type S struct{}
`)
	r := &rule.InstStructRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "add_field",
			Imports: map[string]string{"context": "context"},
		},
		Struct:   "S",
		NewField: []*rule.InstStructField{{Name: "Ctx", Type: "context.Context"}},
	}

	err := newTestPhase().applyStructRule(context.Background(), r, root)

	require.NoError(t, err)
	structType := findStructTypeInFile(t, root, "S")
	require.Len(t, structType.Fields.List, 1)
	field := structType.Fields.List[0]
	sel, ok := field.Type.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", field.Type)
	ident, ok := sel.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "stdctx", ident.Name, "injected field type must use the file's existing alias, not the rule's")
	assert.Equal(t, "Context", sel.Sel.Name)
	assert.Equal(t, 1, countImportSpecs(root), "must not add a redundant import for an alias the rewrite eliminated")
}

func TestApplyStructRule_AliasOverrideUsesResolvedName(t *testing.T) {
	// The target file imports a divergent-name dependency unaliased, so the
	// override must use ip.importNames' resolved real name, not a guess
	// derived from the import path.
	const importPath = "github.com/redis/go-redis/v9"
	root := parseFile(t, `package main

import "`+importPath+`"

type S struct{}
`)
	r := &rule.InstStructRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "add_field",
			Imports: map[string]string{"traced": importPath},
		},
		Struct:   "S",
		NewField: []*rule.InstStructField{{Name: "Client", Type: "*traced.Client"}},
	}

	ip := newTestPhase()
	ip.importNames = map[string]string{importPath: "redis"}

	err := ip.applyStructRule(context.Background(), r, root)

	require.NoError(t, err)
	structType := findStructTypeInFile(t, root, "S")
	require.Len(t, structType.Fields.List, 1)
	field := structType.Fields.List[0]
	star, ok := field.Type.(*dst.StarExpr)
	require.True(t, ok, "expected *dst.StarExpr, got %T", field.Type)
	sel, ok := star.X.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", star.X)
	ident, ok := sel.X.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "redis", ident.Name, "override must use the resolved real name, not the path-derived guess")
	assert.Equal(t, "Client", sel.Sel.Name)
	assert.Equal(t, 1, countImportSpecs(root), "must not add a redundant import for an alias the rewrite eliminated")
}

func TestApplyStructRule_DotImportConflictSurfacesAsAnError(t *testing.T) {
	root := parseFile(t, `package main

import rt "runtime"

type S struct{}
`)
	r := &rule.InstStructRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "add_field",
			Imports: map[string]string{".": "runtime"},
		},
		Struct:   "S",
		NewField: []*rule.InstStructField{{Name: "X", Type: "int"}},
	}

	err := newTestPhase().applyStructRule(context.Background(), r, root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dot-import conflict")
}

// findStructTypeInFile returns the named struct type declared at file scope.
func findStructTypeInFile(t *testing.T, root *dst.File, name string) *dst.StructType {
	t.Helper()
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, specOk := spec.(*dst.TypeSpec)
			if !specOk || typeSpec.Name.Name != name {
				continue
			}
			if st, structOk := typeSpec.Type.(*dst.StructType); structOk {
				return st
			}
		}
	}
	require.Fail(t, "struct type not found", "name: %s", name)
	return nil
}
