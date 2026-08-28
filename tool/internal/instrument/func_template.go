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
	funcDecl      *dst.FuncDecl
	directiveArgs []ast.DirectiveArg
	imports       map[string]string
	hash          string

	fullArgsCollected bool
	fullArgs          []string

	argsCollected bool
	args          []string

	retsCollected bool
	rets          []string
}

// newFuncTemplateData builds the template data for funcDecl. directiveArgs
// are the key:value arguments parsed from the directive comment that matched
// funcDecl (nil for callers that don't have directive args, e.g. raw rules).
// imports resolves the local identifiers used in funcDecl's enclosing file to
// their real import paths (see ast.ImportAliasMap); if nil, FuncArgumentOfType
// and FuncReturnOfType fall back to matching against a type name's package tail.
// hash salts synthetic argument/return names the same way InstFuncRule.Identity
// salts func-rule trampoline names (see collectArguments/collectReturnValues),
// so multiple rules touching the same function never generate colliding names.
func newFuncTemplateData(
	funcDecl *dst.FuncDecl, directiveArgs []ast.DirectiveArg, imports map[string]string, hash string,
) *funcTemplateData {
	return &funcTemplateData{funcDecl: funcDecl, directiveArgs: directiveArgs, imports: imports, hash: hash}
}

// FuncName returns the matched function's name. Template usage: {{.FuncName}}
func (d *funcTemplateData) FuncName() string {
	return d.funcDecl.Name.Name
}

// fullArguments returns the matched function's parameter identifiers,
// including the receiver (as element 0) if present.
func (d *funcTemplateData) fullArguments() []string {
	if !d.fullArgsCollected {
		d.fullArgs = collectArguments(d.funcDecl, d.hash)
		d.fullArgsCollected = true
	}
	return d.fullArgs
}

// arguments returns the matched function's parameter identifiers, excluding
// the receiver.
func (d *funcTemplateData) arguments() []string {
	if !d.argsCollected {
		args := d.fullArguments()
		if ast.HasReceiver(d.funcDecl) {
			args = args[1:]
		}
		d.args = args
		d.argsCollected = true
	}
	return d.args
}

// Receiver returns the identifier of the target method's receiver, or an
// error if the target is a plain function with no receiver. Template usage:
// {{.Receiver}}
func (d *funcTemplateData) Receiver() (string, error) {
	if !ast.HasReceiver(d.funcDecl) {
		return "", ex.Newf("Receiver: function %s has no receiver", d.funcDecl.Name.Name)
	}
	return d.fullArguments()[0], nil
}

func (d *funcTemplateData) returns() []string {
	if !d.retsCollected {
		d.rets = collectReturnValues(d.funcDecl, d.hash)
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
		matched, err := ast.MatchesTypeName(field.Type, typeStr, d.imports)
		if err != nil {
			return "", err
		}
		if matched {
			return args[idx], nil
		}
		idx += len(field.Names)
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
		matched, err := ast.MatchesTypeName(field.Type, typeStr, d.imports)
		if err != nil {
			return "", err
		}
		if matched {
			return rets[idx], nil
		}
		idx += len(field.Names)
	}
	return "", nil
}

// DirectiveArgs returns the key:value arguments parsed from the directive
// comment that matched this function. Template usage:
// {{ range .DirectiveArgs }}{{.Key}}={{.Value}} {{ end }}
func (d *funcTemplateData) DirectiveArgs() []ast.DirectiveArg {
	return d.directiveArgs
}

// DirectiveArg returns the value of the first directive argument with the
// given key, or "" if the directive comment has no such argument. Template
// usage: {{ .DirectiveArg "span.name" }}
func (d *funcTemplateData) DirectiveArg(key string) string {
	for _, arg := range d.directiveArgs {
		if arg.Key == key {
			return arg.Value
		}
	}
	return ""
}
