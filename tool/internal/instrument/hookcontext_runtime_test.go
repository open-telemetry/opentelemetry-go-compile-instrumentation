// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/ast"
)

func TestImplTemplate_ParseAndMaterialize(t *testing.T) {
	p := ast.NewAstParser()
	astRoot, err := p.ParseSource(templateImpl)
	require.NoError(t, err)
	require.NotNil(t, astRoot)

	// Verify that GetKeyData, SetKeyData, and HasKeyData methods are present in templateImpl
	foundGetKeyData := false
	foundSetKeyData := false
	foundHasKeyData := false

	for _, decl := range astRoot.Decls {
		if funcDecl, ok := decl.(*dst.FuncDecl); ok {
			switch funcDecl.Name.Name {
			case "GetKeyData":
				foundGetKeyData = true
			case "SetKeyData":
				foundSetKeyData = true
			case "HasKeyData":
				foundHasKeyData = true
			}
		}
	}

	require.True(t, foundGetKeyData, "GetKeyData must be present in impl.tmpl")
	require.True(t, foundSetKeyData, "SetKeyData must be present in impl.tmpl")
	require.True(t, foundHasKeyData, "HasKeyData must be present in impl.tmpl")
}
