// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"gopkg.in/yaml.v3"

	"go.opentelemetry.io/otelc/tool/ex"
)

// Modifier names recognized in a rule's do clause. The modifier name declares
// the rule type (ADR-0003); dispatch keys on it directly rather than inferring
// the type from which fields are present.
const (
	ModInjectHooks     = "inject_hooks"
	ModInjectCode      = "inject_code"
	ModAddStructFields = "add_struct_fields"
	ModAddFile         = "add_file"
	ModWrapCall        = "wrap_call"
	ModExpandDirective = "expand_directive"
	ModAssignValue     = "assign_value"
)

// The action types below are the typed payloads of each do modifier. They are
// the single home for a modifier's write-intent fields and, together with
// WhereDef, form the structured surface a JSON-schema generator can reflect
// over. Their yaml/json tags mirror the corresponding fields on the concrete
// InstRule structs so a builder can copy across without a marshal round-trip.

// HookAction is the payload of the inject_hooks modifier (InstFuncRule).
type HookAction struct {
	Before string `json:"before,omitempty" yaml:"before,omitempty"` // hook injected at function entry
	After  string `json:"after,omitempty"  yaml:"after,omitempty"`  // hook injected at function exit
	Path   string `json:"path,omitempty"   yaml:"path,omitempty"`   // import path the hooks are loaded from
}

// CodeAction is the payload of the inject_code modifier (InstRawRule).
type CodeAction struct {
	Raw string `json:"raw,omitempty" yaml:"raw,omitempty"` // raw Go source injected into the target function
}

// CallAction is the payload of the wrap_call modifier (InstCallRule).
type CallAction struct {
	Replace      string   `json:"replace,omitempty"       yaml:"replace,omitempty"`       // wrapper expression; {{ . }} is the original call
	AppendArgs   []string `json:"append_args,omitempty"   yaml:"append_args,omitempty"`   // extra arguments appended to the matched call
	VariadicType string   `json:"variadic_type,omitempty" yaml:"variadic_type,omitempty"` // element type of a spread variadic argument
}

// StructAction is the payload of the add_struct_fields modifier (InstStructRule).
type StructAction struct {
	NewField []*InstStructField `json:"new_field,omitempty" yaml:"new_field,omitempty"` // fields to add to the target struct
}

// FileAction is the payload of the add_file modifier (InstFileRule).
type FileAction struct {
	File string `json:"file,omitempty" yaml:"file,omitempty"` // name of the file to add to the target package
	Path string `json:"path,omitempty" yaml:"path,omitempty"` // import path the file is loaded from
}

// DirectiveAction is the payload of the expand_directive modifier (InstDirectiveRule).
type DirectiveAction struct {
	Template string `json:"template,omitempty" yaml:"template,omitempty"` // template rendered into matching function bodies
}

// DeclAction is the payload of the assign_value modifier (InstDeclRule).
type DeclAction struct {
	Replace string `json:"replace,omitempty" yaml:"replace,omitempty"` // expression assigned as the declaration's value
	Wrap    string `json:"wrap,omitempty"    yaml:"wrap,omitempty"`    // template wrapping the existing initializer
}

// DoDef is a single do entry: a discriminated union over the modifier types.
// Exactly one field is non-nil, and that field selects the rule type. It is the
// structured, dispatch-ready replacement for the old field-presence inference.
type DoDef struct {
	InjectHooks     *HookAction      `json:"inject_hooks,omitempty"      yaml:"inject_hooks,omitempty"`
	InjectCode      *CodeAction      `json:"inject_code,omitempty"       yaml:"inject_code,omitempty"`
	AddStructFields *StructAction    `json:"add_struct_fields,omitempty" yaml:"add_struct_fields,omitempty"`
	AddFile         *FileAction      `json:"add_file,omitempty"          yaml:"add_file,omitempty"`
	WrapCall        *CallAction      `json:"wrap_call,omitempty"         yaml:"wrap_call,omitempty"`
	ExpandDirective *DirectiveAction `json:"expand_directive,omitempty"  yaml:"expand_directive,omitempty"`
	AssignValue     *DeclAction      `json:"assign_value,omitempty"      yaml:"assign_value,omitempty"`
}

// count reports how many modifier fields are set.
func (d *DoDef) count() int {
	n := 0
	for _, set := range []bool{
		d.InjectHooks != nil, d.InjectCode != nil, d.AddStructFields != nil,
		d.AddFile != nil, d.WrapCall != nil, d.ExpandDirective != nil,
		d.AssignValue != nil,
	} {
		if set {
			n++
		}
	}
	return n
}

// validate enforces the single-modifier-per-entry invariant.
func (d *DoDef) validate() error {
	switch d.count() {
	case 1:
		return nil
	case 0:
		return ex.Newf("do entry must name exactly one modifier (one of: %s, %s, %s, %s, %s, %s, %s)",
			ModInjectHooks, ModInjectCode, ModAddStructFields, ModAddFile,
			ModWrapCall, ModExpandDirective, ModAssignValue)
	default:
		return ex.Newf("do entry must contain exactly one modifier key; " +
			"use the sequence form for multiple modifiers")
	}
}

// DoList is a rule's ordered do clause. Declaration order is preserved and is
// the order in which modifiers are applied.
type DoList []DoDef

// UnmarshalYAML accepts the two documented do shapes and produces the same
// ordered list for both:
//
//   - sequence of single-key modifier maps (canonical, supports N modifiers);
//   - a single-key map (sugar for a one-element sequence).
func (dl *DoList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	// Sequence form (canonical): a list of single-key modifier maps.
	if node.Kind == yaml.SequenceNode {
		if len(node.Content) == 0 {
			return ex.Newf("do must not be empty")
		}
		list := make(DoList, 0, len(node.Content))
		for i, item := range node.Content {
			var d DoDef
			if err := item.Decode(&d); err != nil {
				return ex.Wrapf(err, "do[%d]", i)
			}
			if err := d.validate(); err != nil {
				return ex.Wrapf(err, "do[%d]", i)
			}
			list = append(list, d)
		}
		*dl = list
		return nil
	}
	// Map form (sugar): a single modifier written directly.
	if node.Kind == yaml.MappingNode {
		if len(node.Content) == 0 {
			return ex.Newf("do must not be empty")
		}
		var d DoDef
		if err := node.Decode(&d); err != nil {
			return ex.Wrap(err)
		}
		if err := d.validate(); err != nil {
			return err
		}
		*dl = DoList{d}
		return nil
	}
	return ex.Newf("do must be a single-key map or a non-empty list of single-key modifier objects")
}
