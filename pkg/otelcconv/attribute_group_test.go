// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelcconv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const groupsDir = "../../schemas/otelc/groups"

type registryFile struct {
	Groups []struct {
		Attributes []struct {
			ID         string `yaml:"id"`
			Deprecated any    `yaml:"deprecated"`
		} `yaml:"attributes"`
	} `yaml:"groups"`
}

// declaredAttributes returns every attribute declared with `id:` in the
// registry, read straight from the YAML rather than through weaver.
func declaredAttributes(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(groupsDir, "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no registry files under %s", groupsDir)

	var ids []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		require.NoError(t, err)

		var rf registryFile
		require.NoError(t, yaml.Unmarshal(data, &rf), f)
		for _, g := range rf.Groups {
			for _, a := range g.Attributes {
				if a.ID != "" && a.Deprecated == nil {
					ids = append(ids, a.ID)
				}
			}
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// generatedAttributes returns the attribute names attribute_group.go defines
// keys for, read from its syntax tree.
func generatedAttributes(t *testing.T) []string {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), "attribute_group.go", nil, 0)
	require.NoError(t, err)

	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Key" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "attribute" {
			return true
		}
		if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			name, err := strconv.Unquote(lit.Value)
			require.NoError(t, err)
			names = append(names, name)
		}
		return true
	})
	slices.Sort(names)
	return names
}

// TestGeneratedMatchesRegistryIDs guards the lineage filter in
// schemas/templates/registry/go/weaver.yaml. That filter relies on weaver's
// lineage output, which weaver does not document as stable, so this checks the
// result against the `id:` entries parsed directly from the YAML. If it fails
// after a registry change, run `make generate`; if it fails after a weaver
// upgrade, the filter needs fixing.
func TestGeneratedMatchesRegistryIDs(t *testing.T) {
	assert.Equal(t, declaredAttributes(t), generatedAttributes(t))
}
