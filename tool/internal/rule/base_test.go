// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstBaseRuleAccessors(t *testing.T) {
	where := &WhereDef{Func: "Foo"}
	base := &InstBaseRule{
		Name:    "myrule",
		Target:  "example.com/pkg",
		Version: "v1.0.0,v2.0.0",
		Where:   where,
	}

	assert.Equal(t, "myrule", base.String())
	assert.Equal(t, "myrule", base.GetName())
	assert.Equal(t, "example.com/pkg", base.GetTarget())
	assert.Equal(t, "v1.0.0,v2.0.0", base.GetVersion())
	assert.Same(t, where, base.GetWhere())

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
