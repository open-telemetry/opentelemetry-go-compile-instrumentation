// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestInstBaseRuleAccessors(t *testing.T) {
	where := &WhereDef{Func: "Foo"}
	base := &InstBaseRule{
		Name:    "myrule",
		Target:  NewTarget("example.com/pkg"),
		Version: "v1.0.0,v2.0.0",
		Where:   where,
	}

	assert.Equal(t, "myrule", base.String())
	assert.Equal(t, "myrule", base.GetName())
	assert.Equal(t, "example.com/pkg", base.GetTarget().String())
	assert.Equal(t, "v1.0.0,v2.0.0", base.GetVersion())
	assert.Same(t, where, base.GetWhere())
	assert.Same(t, base, base.Base())
	funcRule := &InstFuncRule{InstBaseRule: *base}
	assert.Same(t, &funcRule.InstBaseRule, funcRule.Base(), "a rule kind reaches its own embedded base")

	// InstBaseRule satisfies the InstRule interface.
	var _ InstRule = base
}

func TestNewInstRuleSet(t *testing.T) {
	irs := NewInstRuleSet("example.com/mod")
	require.NotNil(t, irs)

	assert.Equal(t, "example.com/mod", irs.ModulePath)
	assert.Empty(t, irs.PackageName)
	assert.NotNil(t, irs.CgoFileMap)
	assert.NotNil(t, irs.RawRules)
	assert.NotNil(t, irs.FuncRules)
	assert.NotNil(t, irs.StructRules)
	assert.NotNil(t, irs.CallRules)
	assert.NotNil(t, irs.DirectiveRules)
	assert.NotNil(t, irs.DeclRules)
	assert.NotNil(t, irs.FileRules)

	// A freshly created rule set has no rules.
	assert.True(t, irs.IsEmpty())
}

func TestInstRuleSetIsEmpty(t *testing.T) {
	// A nil rule set is considered empty.
	var nilSet *InstRuleSet
	assert.True(t, nilSet.IsEmpty())

	file := filepath.Join(t.TempDir(), "a.go")

	t.Run("func rule makes it non-empty", func(t *testing.T) {
		irs := NewInstRuleSet("m")
		irs.AddFuncRule(file, &InstFuncRule{})
		assert.False(t, irs.IsEmpty())
	})
	t.Run("file rule makes it non-empty", func(t *testing.T) {
		irs := NewInstRuleSet("m")
		irs.AddFileRule(&InstFileRule{})
		assert.False(t, irs.IsEmpty())
	})
	t.Run("struct rule makes it non-empty", func(t *testing.T) {
		irs := NewInstRuleSet("m")
		irs.AddStructRule(file, &InstStructRule{})
		assert.False(t, irs.IsEmpty())
	})
	t.Run("ignored call file makes it non-empty", func(t *testing.T) {
		irs := NewInstRuleSet("m")
		irs.AddIgnoredCallFile(file)
		assert.False(t, irs.IsEmpty())
	})
}

func TestInstRuleSetAdders(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.go")
	fileB := filepath.Join(dir, "b.go")

	irs := NewInstRuleSet("example.com/mod")

	irs.AddRawRule(fileA, &InstRawRule{Raw: "println()"})
	irs.AddFuncRule(fileA, &InstFuncRule{Func: "F1"})
	irs.AddFuncRule(fileA, &InstFuncRule{Func: "F2"})
	irs.AddFuncRule(fileB, &InstFuncRule{Func: "F3"})
	irs.AddStructRule(fileA, &InstStructRule{Struct: "S1"})
	irs.AddStructRule(fileB, &InstStructRule{Struct: "S2"})
	irs.AddCallRule(fileA, &InstCallRule{})
	irs.AddDirectiveRule(fileA, &InstDirectiveRule{})
	irs.AddDeclRule(fileA, &InstDeclRule{})
	irs.AddFileRule(&InstFileRule{})

	assert.Len(t, irs.RawRules[fileA], 1)
	assert.Len(t, irs.FuncRules[fileA], 2)
	assert.Len(t, irs.FuncRules[fileB], 1)
	assert.Len(t, irs.StructRules[fileA], 1)
	assert.Len(t, irs.CallRules[fileA], 1)
	assert.Len(t, irs.DirectiveRules[fileA], 1)
	assert.Len(t, irs.DeclRules[fileA], 1)
	assert.Len(t, irs.FileRules, 1)

	// AllFuncRules flattens every file's func rules.
	allFuncs := irs.AllFuncRules()
	assert.Len(t, allFuncs, 3)

	// AllStructRules flattens every file's struct rules.
	allStructs := irs.AllStructRules()
	assert.Len(t, allStructs, 2)
}

func TestInstRuleSetAllRulesEmpty(t *testing.T) {
	irs := NewInstRuleSet("m")
	assert.Empty(t, irs.AllFuncRules())
	assert.Empty(t, irs.AllStructRules())
	assert.Empty(t, collectAllRules(irs))

	var nilSet *InstRuleSet
	assert.Empty(t, collectAllRules(nilSet))
}

func TestInstRuleSetSetters(t *testing.T) {
	irs := NewInstRuleSet("m")

	irs.SetPackageName("mypkg")
	assert.Equal(t, "mypkg", irs.PackageName)

	cgo := map[string]string{"a.go": "a.cgo1.go"}
	irs.SetCgoFileMap(cgo)
	assert.Equal(t, cgo, irs.CgoFileMap)
}

func TestInstRuleSetString(t *testing.T) {
	irs := NewInstRuleSet("example.com/mod")
	s := irs.String()
	// The string representation includes the module path and each rule bucket.
	assert.Contains(t, s, "example.com/mod")
	assert.Contains(t, s, "raw=")
	assert.Contains(t, s, "func=")
	assert.Contains(t, s, "struct=")
	assert.Contains(t, s, "call=")
	assert.Contains(t, s, "directive=")
	assert.Contains(t, s, "decl=")
	assert.Contains(t, s, "file=")
}

// TestInstRuleSetAllRulesStableOrder checks that both flatteners order their
// output by source file rather than by map iteration. Files are registered out
// of name order so insertion order cannot pass by accident.
func TestInstRuleSetAllRulesStableOrder(t *testing.T) {
	dir := t.TempDir()
	// Added out of order; expected results below are sorted by file path.
	files := []string{
		filepath.Join(dir, "d.go"),
		filepath.Join(dir, "b.go"),
		filepath.Join(dir, "a.go"),
		filepath.Join(dir, "c.go"),
	}
	wantFuncs := []string{"a", "b", "c", "d"}
	wantStructs := []string{"A", "B", "C", "D"}

	const iterations = 100
	for range iterations {
		irs := NewInstRuleSet("example.com/mod")
		for _, file := range files {
			name := strings.TrimSuffix(filepath.Base(file), ".go")
			irs.AddFuncRule(file, &InstFuncRule{Func: name})
			irs.AddStructRule(file, &InstStructRule{Struct: strings.ToUpper(name)})
		}

		gotFuncs := make([]string, 0, len(files))
		for _, r := range irs.AllFuncRules() {
			gotFuncs = append(gotFuncs, r.Func)
		}
		gotStructs := make([]string, 0, len(files))
		for _, r := range irs.AllStructRules() {
			gotStructs = append(gotStructs, r.Struct)
		}

		require.Equal(t, wantFuncs, gotFuncs, "AllFuncRules order must not depend on map iteration")
		require.Equal(t, wantStructs, gotStructs, "AllStructRules order must not depend on map iteration")
	}
}

func TestInstRuleSetAllRulesIncludesEveryKindInStableOrder(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.go")
	fileB := filepath.Join(dir, "b.go")
	rawA := &InstRawRule{Raw: "a"}
	rawB := &InstRawRule{Raw: "b"}
	funcRule := &InstFuncRule{Func: "f"}
	structRule := &InstStructRule{Struct: "s"}
	callRule := &InstCallRule{}
	litRule := &InstLitRule{}
	directiveRule := &InstDirectiveRule{}
	declRule := &InstDeclRule{}
	fileRule := &InstFileRule{}

	irs := NewInstRuleSet("example.com/mod")
	irs.AddRawRule(fileB, rawB)
	irs.AddRawRule(fileA, rawA)
	irs.AddFuncRule(fileA, funcRule)
	irs.AddStructRule(fileA, structRule)
	irs.AddCallRule(fileA, callRule)
	irs.AddLitRule(fileA, litRule)
	irs.AddDirectiveRule(fileA, directiveRule)
	irs.AddDeclRule(fileA, declRule)
	irs.AddFileRule(fileRule)

	want := []InstRule{
		rawA,
		rawB,
		funcRule,
		structRule,
		callRule,
		litRule,
		directiveRule,
		declRule,
		fileRule,
	}
	assert.Equal(t, want, collectAllRules(irs))

	for stopAfter := 1; stopAfter <= len(want); stopAfter++ {
		t.Run(fmt.Sprintf("stop after rule %d", stopAfter), func(t *testing.T) {
			var got []InstRule
			for r := range irs.AllRules() {
				got = append(got, r)
				if len(got) == stopAfter {
					break
				}
			}
			assert.Equal(t, want[:stopAfter], got)
		})
	}
}

func collectAllRules(irs *InstRuleSet) []InstRule {
	var result []InstRule
	for rule := range irs.AllRules() {
		result = append(result, rule)
	}
	return result
}

func TestInstRuleSetIsInternal(t *testing.T) {
	var nilSet *InstRuleSet
	assert.False(t, nilSet.IsInternal())

	emptySet := NewInstRuleSet("example.com/mod")
	assert.False(t, emptySet.IsInternal())

	internalFileSet := NewInstRuleSet("runtime")
	internalFileSet.AddFileRule(&InstFileRule{Path: util.InternalPath})
	assert.True(t, internalFileSet.IsInternal())

	file := filepath.Join(t.TempDir(), "file.go")
	internalFuncSet := NewInstRuleSet("runtime")
	internalFuncSet.AddFuncRule(file, &InstFuncRule{Path: util.InternalPath})
	assert.True(t, internalFuncSet.IsInternal())

	externalSet := NewInstRuleSet("example.com/mod")
	externalSet.AddFileRule(&InstFileRule{Path: "example.com/hooks"})
	assert.False(t, externalSet.IsInternal())
}
