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
