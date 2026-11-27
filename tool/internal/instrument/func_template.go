// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/types"

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
	// typeInfo gives the go/types view of funcDecl for the ThatImplements
	// helpers. When nil, they match the type name only.
	typeInfo *funcTypes

	fullArgsCollected bool
	fullArgs          []string

	argsCollected bool
	args          []string

	retsCollected bool
	rets          []string

	returnedIdents map[string]bool
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

// funcLitDecl returns a FuncDecl view of lit, so that funcTemplateData and
// the helpers that take a FuncDecl also work for a function literal. The view
// has an empty name and no receiver. It shares lit's Type and Body, so names
// given to unnamed parameters and results, and statements prepended to the
// body, change lit.
func funcLitDecl(lit *dst.FuncLit) *dst.FuncDecl {
	return &dst.FuncDecl{Name: dst.NewIdent(""), Type: lit.Type, Body: lit.Body}
}

// FuncName returns the matched function's name, or "" for a function literal
// (see funcLitDecl). Template usage: {{.FuncName}}
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

func (d *funcTemplateData) recordTargetIdent(name string) string {
	if name == "" {
		return ""
	}
	if d.returnedIdents == nil {
		d.returnedIdents = make(map[string]bool)
	}
	d.returnedIdents[name] = true
	return name
}

func (d *funcTemplateData) hasOverlap(aliases map[string]bool) bool {
	if d == nil || len(d.returnedIdents) == 0 || len(aliases) == 0 {
		return false
	}
	for alias := range aliases {
		if d.returnedIdents[alias] {
			return true
		}
	}
	return false
}

// Receiver returns the identifier of the target method's receiver, or an
// error if the target is a plain function with no receiver. Template usage:
// {{.Receiver}}
func (d *funcTemplateData) Receiver() (string, error) {
	if !ast.HasReceiver(d.funcDecl) {
		return "", ex.Newf("Receiver: function %s has no receiver", d.funcDecl.Name.Name)
	}
	return d.recordTargetIdent(d.fullArguments()[0]), nil
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
	return d.recordTargetIdent(args[idx]), nil
}

// FuncReturn returns the identifier of the idx-th (0-indexed) return value.
// Template usage: {{.FuncReturn N}}
func (d *funcTemplateData) FuncReturn(idx int) (string, error) {
	rets := d.returns()
	if idx < 0 || idx >= len(rets) {
		return "", ex.Newf("FuncReturn index %d out of range [0, %d)", idx, len(rets))
	}
	return d.recordTargetIdent(rets[idx]), nil
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
			return d.recordTargetIdent(args[idx]), nil
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
			return d.recordTargetIdent(rets[idx]), nil
		}
		idx += len(field.Names)
	}
	return "", nil
}

// FuncArgumentThatImplements returns the identifier of the first parameter
// (excluding the receiver) whose type is the interface iface or implements
// it, or "" if none does. Template usage:
// {{ .FuncArgumentThatImplements "context.Context" }}
func (d *funcTemplateData) FuncArgumentThatImplements(iface string) (string, error) {
	args := d.arguments() // ensures synthetic names are assigned first
	var vars *types.Tuple
	if sig := d.typeInfo.signature(d.funcDecl); sig != nil {
		vars = sig.Params()
	}
	idx, err := d.typeInfo.firstImplementing(typedSlots(d.funcDecl.Type.Params, vars), iface, d.imports)
	if err != nil || idx < 0 {
		return "", err
	}
	return d.recordTargetIdent(args[idx]), nil
}

// FuncReturnThatImplements returns the identifier of the first return value
// whose type is the interface iface or implements it, or "" if none does.
// Template usage: {{ .FuncReturnThatImplements "error" }}
func (d *funcTemplateData) FuncReturnThatImplements(iface string) (string, error) {
	rets := d.returns() // ensures synthetic names are assigned first
	var vars *types.Tuple
	if sig := d.typeInfo.signature(d.funcDecl); sig != nil {
		vars = sig.Results()
	}
	idx, err := d.typeInfo.firstImplementing(typedSlots(d.funcDecl.Type.Results, vars), iface, d.imports)
	if err != nil || idx < 0 {
		return "", err
	}
	return d.recordTargetIdent(rets[idx]), nil
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

// maskedFuncTemplateData substitutes target-derived identifier helpers with a
// placeholder so re-rendering can identify rule aliases written by the template
// itself without counting matching parameter, receiver, or return names.
type maskedFuncTemplateData struct {
	*funcTemplateData

	placeholder string
}

func (m *maskedFuncTemplateData) maskTargetIdent(val string, err error) (string, error) {
	if err != nil || val == "" {
		return val, err
	}
	return m.placeholder, nil
}

func (m *maskedFuncTemplateData) Receiver() (string, error) {
	return m.maskTargetIdent(m.funcTemplateData.Receiver())
}

func (m *maskedFuncTemplateData) FuncArgument(idx int) (string, error) {
	return m.maskTargetIdent(m.funcTemplateData.FuncArgument(idx))
}

func (m *maskedFuncTemplateData) FuncReturn(idx int) (string, error) {
	return m.maskTargetIdent(m.funcTemplateData.FuncReturn(idx))
}

func (m *maskedFuncTemplateData) FuncArgumentOfType(typeStr string) (string, error) {
	return m.maskTargetIdent(m.funcTemplateData.FuncArgumentOfType(typeStr))
}

func (m *maskedFuncTemplateData) FuncReturnOfType(typeStr string) (string, error) {
	return m.maskTargetIdent(m.funcTemplateData.FuncReturnOfType(typeStr))
}

func (m *maskedFuncTemplateData) FuncArgumentThatImplements(iface string) (string, error) {
	return m.maskTargetIdent(m.funcTemplateData.FuncArgumentThatImplements(iface))
}

func (m *maskedFuncTemplateData) FuncReturnThatImplements(iface string) (string, error) {
	return m.maskTargetIdent(m.funcTemplateData.FuncReturnThatImplements(iface))
}
