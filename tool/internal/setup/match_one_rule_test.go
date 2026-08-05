// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"path/filepath"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

const matchOneRuleSource = `package sample

const MaxRetries = 3

type Widget struct{ x int }

//sample:trace
func Traced() {}

func Plain() {}
`

func parseMatchSource(t *testing.T) *dst.File {
	t.Helper()
	tree, err := ast.NewAstParser().ParseSource(matchOneRuleSource)
	require.NoError(t, err)
	return tree
}

func TestMatchOneRule(t *testing.T) {
	source := filepath.Join(t.TempDir(), "sample.go")
	dep := &Dependency{ImportPath: "example.com/sample"}

	tests := []struct {
		name   string
		rule   rule.InstRule
		verify func(*testing.T, *rule.InstRuleSet)
	}{
		{
			name: "func rule matches declared function",
			rule: &rule.InstFuncRule{
				InstBaseRule: rule.InstBaseRule{Target: "example.com/sample"},
				Func:         "Plain",
				Before:       "H",
				Path:         "example.com/hooks",
			},
			verify: func(t *testing.T, set *rule.InstRuleSet) {
				assert.Len(t, set.FuncRules[source], 1)
			},
		},
		{
			name: "func rule does not match missing function",
			rule: &rule.InstFuncRule{
				InstBaseRule: rule.InstBaseRule{Target: "example.com/sample"},
				Func:         "DoesNotExist",
				Before:       "H",
				Path:         "example.com/hooks",
			},
			verify: func(t *testing.T, set *rule.InstRuleSet) {
				assert.Empty(t, set.FuncRules[source])
			},
		},
		{
			name: "struct rule matches declared struct",
			rule: &rule.InstStructRule{
				InstBaseRule: rule.InstBaseRule{Target: "example.com/sample"},
				Struct:       "Widget",
			},
			verify: func(t *testing.T, set *rule.InstRuleSet) {
				assert.Len(t, set.StructRules[source], 1)
			},
		},
		{
			name: "raw rule matches declared function",
			rule: &rule.InstRawRule{
				InstBaseRule: rule.InstBaseRule{Target: "example.com/sample"},
				Func:         "Plain",
				Raw:          "println()",
			},
			verify: func(t *testing.T, set *rule.InstRuleSet) {
				assert.Len(t, set.RawRules[source], 1)
			},
		},
		{
			name: "call rule is added unconditionally",
			rule: &rule.InstCallRule{
				InstBaseRule: rule.InstBaseRule{Target: "example.com/sample"},
				FunctionCall: "net/http.Get",
			},
			verify: func(t *testing.T, set *rule.InstRuleSet) {
				assert.Len(t, set.CallRules[source], 1)
			},
		},
		{
			name: "directive rule matches annotated function",
			rule: &rule.InstDirectiveRule{
				InstBaseRule: rule.InstBaseRule{Target: "example.com/sample"},
				Directive:    "sample:trace",
			},
			verify: func(t *testing.T, set *rule.InstRuleSet) {
				assert.Len(t, set.DirectiveRules[source], 1)
			},
		},
		{
			name: "decl rule matches named const",
			rule: &rule.InstDeclRule{
				InstBaseRule: rule.InstBaseRule{Target: "example.com/sample"},
				Identifier:   "MaxRetries",
				Kind:         "const",
			},
			verify: func(t *testing.T, set *rule.InstRuleSet) {
				assert.Len(t, set.DeclRules[source], 1)
			},
		},
		{
			name: "file rule is skipped",
			rule: &rule.InstFileRule{
				InstBaseRule: rule.InstBaseRule{Target: "example.com/sample"},
			},
			verify: func(t *testing.T, set *rule.InstRuleSet) {
				assert.Empty(t, set.FileRules)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := newTestSetupPhase()
			set := rule.NewInstRuleSet("example.com/sample")
			tree := parseMatchSource(t)

			require.NoError(t, sp.matchOneRule(tree, source, tt.rule, set, dep))
			tt.verify(t, set)
		})
	}
}
