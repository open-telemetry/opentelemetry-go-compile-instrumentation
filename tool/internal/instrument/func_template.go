// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
)

// funcTemplateData lazily exposes a matched function's name, arguments, and
// return values to text/template renderers. Argument and return collection
// mutates the underlying FuncDecl.
type funcTemplateData struct {
	funcDecl *dst.FuncDecl

	argsCollected bool
	args          []string

	retsCollected bool
	rets          []string
}

func newFuncTemplateData(funcDecl *dst.FuncDecl) *funcTemplateData {
	return &funcTemplateData{funcDecl: funcDecl}
}

// FuncName returns the matched function's name. Template usage: {{.FuncName}}
func (d *funcTemplateData) FuncName() string {
	return d.funcDecl.Name.Name
}

// arguments returns the matched function's parameter identifiers, excluding
// the receiver.
func (d *funcTemplateData) arguments() []string {
	if !d.argsCollected {
		args := collectArguments(d.funcDecl)
		if ast.HasReceiver(d.funcDecl) {
			args = args[1:]
		}
		d.args = args
		d.argsCollected = true
	}
	return d.args
}

func (d *funcTemplateData) returns() []string {
	if !d.retsCollected {
		d.rets = collectReturnValues(d.funcDecl)
		d.retsCollected = true
	}
	return d.rets
}

// FuncArgument returns the identifier of the idx-th (0-indexed) parameter,
// excluding the receiver. Template usage: {{.FuncArgument N}}
func (d *funcTemplateData) FuncArgument(idx int) (string, error) {
	args := d.arguments()
	if idx < 0 || idx >= len(args) {
		return "", ex.Newf("FuncArgument index %d out of range [0, %d)", idx, len(args))
	}
	return args[idx], nil
}

// FuncReturn returns the identifier of the idx-th (0-indexed) return value.
// Template usage: {{.FuncReturn N}}
func (d *funcTemplateData) FuncReturn(idx int) (string, error) {
	rets := d.returns()
	if idx < 0 || idx >= len(rets) {
		return "", ex.Newf("FuncReturn index %d out of range [0, %d)", idx, len(rets))
	}
	return rets[idx], nil
}

// FuncArgumentCount returns the number of parameters, excluding the
// receiver. Template usage: {{.FuncArgumentCount}}
func (d *funcTemplateData) FuncArgumentCount() int {
	return len(d.arguments())
}

// FuncReturnCount returns the number of return values. Template usage:
// {{.FuncReturnCount}}
func (d *funcTemplateData) FuncReturnCount() int {
	return len(d.returns())
}

// FuncArgumentOfType returns the identifier of the first parameter (excluding
// the receiver) whose type matches typeStr, or "" if none match. Template
// usage: {{ .FuncArgumentOfType "context.Context" }}
func (d *funcTemplateData) FuncArgumentOfType(typeStr string) (string, error) {
	args := d.arguments() // ensures synthetic names are assigned first
	idx := 0
	for _, field := range d.funcDecl.Type.Params.List {
		for range field.Names {
			matched, err := ast.MatchesTypeName(field.Type, typeStr)
			if err != nil {
				return "", err
			}
			if matched {
				return args[idx], nil
			}
			idx++
		}
	}
	return "", nil
}

// FuncReturnOfType returns the identifier of the first return value whose
// type matches typeStr, or "" if none match. Template usage:
// {{ .FuncReturnOfType "error" }}
func (d *funcTemplateData) FuncReturnOfType(typeStr string) (string, error) {
	rets := d.returns() // ensures synthetic names are assigned first
	if d.funcDecl.Type.Results == nil {
		return "", nil
	}
	idx := 0
	for _, field := range d.funcDecl.Type.Results.List {
		for range field.Names {
			matched, err := ast.MatchesTypeName(field.Type, typeStr)
			if err != nil {
				return "", err
			}
			if matched {
				return rets[idx], nil
			}
			idx++
		}
	}
	return "", nil
}
