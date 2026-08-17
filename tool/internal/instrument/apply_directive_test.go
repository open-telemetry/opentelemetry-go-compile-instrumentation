// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"log/slog"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestApplyDirectiveRule(t *testing.T) {
	t.Run("successfully applies directive to function with body", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{Name: "test_directive"},
			Directive:    "otelc:test",
			Template:     "println(\"{{FuncName}}\")",
		}
		funcDecl := &dst.FuncDecl{
			Name: dst.NewIdent("myFunc"),
			Type: &dst.FuncType{
				Params: &dst.FieldList{},
			},
			Body: &dst.BlockStmt{
				List: []dst.Stmt{
					&dst.ExprStmt{
						X: &dst.CallExpr{
							Fun: dst.NewIdent("println"),
							Args: []dst.Expr{
								&dst.BasicLit{Kind: token.STRING, Value: `"body"`},
							},
						},
					},
				},
			},
			Decs: dst.FuncDeclDecorations{
				NodeDecs: dst.NodeDecs{
					Start: dst.Decorations{"//otelc:test\n"},
				},
			},
		}
		root := &dst.File{
			Decls: []dst.Decl{funcDecl},
		}

		ip := &InstrumentPhase{logger: slog.Default()}
		err := ip.applyDirectiveRule(context.Background(), r, root)
		require.NoError(t, err)
		assert.Len(t, funcDecl.Body.List, 2)
	})

	t.Run("returns error on invalid template syntax", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{Name: "test_directive"},
			Directive:    "otelc:test",
			Template:     "println(\"{{FuncName\")",
		}
		root := &dst.File{}

		ip := &InstrumentPhase{logger: slog.Default()}
		err := ip.applyDirectiveRule(context.Background(), r, root)
		require.Error(t, err)
	})

	t.Run("returns error on unknown template tag", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{Name: "test_directive"},
			Directive:    "otelc:test",
			Template:     "println(\"{{UnknownTag}}\")",
		}
		funcDecl := &dst.FuncDecl{
			Name: dst.NewIdent("myFunc"),
			Type: &dst.FuncType{Params: &dst.FieldList{}},
			Body: &dst.BlockStmt{List: []dst.Stmt{}},
			Decs: dst.FuncDeclDecorations{
				NodeDecs: dst.NodeDecs{
					Start: dst.Decorations{"//otelc:test\n"},
				},
			},
		}
		root := &dst.File{Decls: []dst.Decl{funcDecl}}

		ip := &InstrumentPhase{logger: slog.Default()}
		err := ip.applyDirectiveRule(context.Background(), r, root)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown template tag \"UnknownTag\"")
	})

	t.Run("returns error on invalid Go syntax in rendered snippet", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{Name: "test_directive"},
			Directive:    "otelc:test",
			Template:     "println( { {{FuncName}}",
		}
		funcDecl := &dst.FuncDecl{
			Name: dst.NewIdent("myFunc"),
			Type: &dst.FuncType{Params: &dst.FieldList{}},
			Body: &dst.BlockStmt{List: []dst.Stmt{}},
			Decs: dst.FuncDeclDecorations{
				NodeDecs: dst.NodeDecs{
					Start: dst.Decorations{"//otelc:test\n"},
				},
			},
		}
		root := &dst.File{Decls: []dst.Decl{funcDecl}}

		ip := &InstrumentPhase{logger: slog.Default()}
		err := ip.applyDirectiveRule(context.Background(), r, root)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing rendered template")
	})
}
