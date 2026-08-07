// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestAllSelectors(t *testing.T) {
	want := []rule.Selector{
		rule.SelectorFunc,
		rule.SelectorRecv,
		rule.SelectorStruct,
		rule.SelectorFunctionCall,
		rule.SelectorDirective,
		rule.SelectorKind,
		rule.SelectorIdentifier,
		rule.SelectorSignature,
		rule.SelectorSignatureContains,
		rule.SelectorResult,
		rule.SelectorLastResult,
		rule.SelectorParam,
		rule.SelectorPattern,
		rule.SelectorPlacement,
	}

	if diff := cmp.Diff(want, rule.AllSelectors()); diff != "" {
		t.Fatalf("AllSelectors() mismatch (-want +got):\n%s", diff)
	}
}

func TestAllModifiers(t *testing.T) {
	want := []rule.Modifier{
		rule.ModifierInjectHooks,
		rule.ModifierInjectCode,
		rule.ModifierAddStructFields,
		rule.ModifierAddFile,
		rule.ModifierWrapCall,
		rule.ModifierExpandDirective,
		rule.ModifierAssignValue,
	}

	if diff := cmp.Diff(want, rule.AllModifiers()); diff != "" {
		t.Fatalf("AllModifiers() mismatch (-want +got):\n%s", diff)
	}
}

func TestRegistryAccessorsReturnCopies(t *testing.T) {
	selectors := rule.AllSelectors()
	selectors[0] = rule.Selector("bogus")
	if got := rule.AllSelectors()[0]; got != rule.SelectorFunc {
		t.Fatalf("AllSelectors() returned shared backing slice, first selector = %q", got)
	}

	modifiers := rule.AllModifiers()
	modifiers[0] = rule.Modifier("bogus")
	if got := rule.AllModifiers()[0]; got != rule.ModifierInjectHooks {
		t.Fatalf("AllModifiers() returned shared backing slice, first modifier = %q", got)
	}
}

func TestIsValidSelector(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "func selector", value: rule.SelFunc, want: true},
		{name: "signature selector", value: rule.SelSignature, want: true},
		{name: "raw selector", value: rule.SelPattern, want: true},
		{name: "file is structural not selector", value: rule.WhereFile, want: false},
		{name: "typo", value: "fnc", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rule.IsValidSelector(tt.value); got != tt.want {
				t.Fatalf("IsValidSelector(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestIsValidModifier(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "inject hooks", value: string(rule.ModifierInjectHooks), want: true},
		{name: "inject code", value: string(rule.ModifierInjectCode), want: true},
		{name: "assign value", value: string(rule.ModifierAssignValue), want: true},
		{name: "legacy alias not accepted", value: "raw_inject", want: false},
		{name: "typo", value: "inject_hook", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rule.IsValidModifier(tt.value); got != tt.want {
				t.Fatalf("IsValidModifier(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestHasWhereSelectors(t *testing.T) {
	tests := []struct {
		name  string
		where *rule.WhereDef
		want  bool
	}{
		{name: "nil where", want: false},
		{name: "file only", where: &rule.WhereDef{File: &rule.FilterDef{HasFunc: "init"}}, want: false},
		{name: "func selector", where: &rule.WhereDef{Func: "Open"}, want: true},
		{name: "call selector", where: &rule.WhereDef{FunctionCall: "net/http.Get"}, want: true},
		{name: "directive selector", where: &rule.WhereDef{Directive: "otelc:span"}, want: true},
		{name: "kind selector", where: &rule.WhereDef{Kind: "var"}, want: true},
		{name: "identifier selector", where: &rule.WhereDef{Identifier: "DefaultTransport"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rule.HasWhereSelectors(tt.where); got != tt.want {
				t.Fatalf("HasWhereSelectors(%+v) = %v, want %v", tt.where, got, tt.want)
			}
		})
	}
}
