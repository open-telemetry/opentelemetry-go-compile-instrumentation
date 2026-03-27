// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstBaseRule(t *testing.T) {
	r := &InstBaseRule{
		Name:    "my-rule",
		Target:  "example.com/pkg",
		Version: "v1.0.0,v2.0.0",
	}
	assert.Equal(t, "my-rule", r.String())
	assert.Equal(t, "my-rule", r.GetName())
	assert.Equal(t, "example.com/pkg", r.GetTarget())
	assert.Equal(t, "v1.0.0,v2.0.0", r.GetVersion())
}

func TestNewInstRuleSet(t *testing.T) {
	rs := NewInstRuleSet("example.com/pkg")
	require.NotNil(t, rs)
	assert.Equal(t, "example.com/pkg", rs.ModulePath)
	assert.NotNil(t, rs.FuncRules)
	assert.NotNil(t, rs.StructRules)
	assert.NotNil(t, rs.RawRules)
	assert.NotNil(t, rs.CallRules)
	assert.NotNil(t, rs.DirectiveRules)
	assert.NotNil(t, rs.DeclRules)
	assert.NotNil(t, rs.ValueDeclRules)
	assert.NotNil(t, rs.FileRules)
}

func TestInstRuleSet_IsEmpty(t *testing.T) {
	rs := NewInstRuleSet("example.com/pkg")
	assert.True(t, rs.IsEmpty())

	var nilRS *InstRuleSet
	assert.True(t, nilRS.IsEmpty())
}

func TestInstRuleSet_IsEmpty_AfterAddingRule(t *testing.T) {
	file := filepath.Join(os.TempDir(), "file.go")

	t.Run("func rule", func(t *testing.T) {
		rs := NewInstRuleSet("example.com/pkg")
		rs.AddFuncRule(file, &InstFuncRule{InstBaseRule: InstBaseRule{Name: "f"}})
		assert.False(t, rs.IsEmpty())
	})
	t.Run("value decl rule", func(t *testing.T) {
		rs := NewInstRuleSet("example.com/pkg")
		rs.AddValueDeclRule(file, &InstValueDeclRule{InstBaseRule: InstBaseRule{Name: "vd"}})
		assert.False(t, rs.IsEmpty())
	})
	t.Run("file rule", func(t *testing.T) {
		rs := NewInstRuleSet("example.com/pkg")
		rs.AddFileRule(&InstFileRule{InstBaseRule: InstBaseRule{Name: "file"}})
		assert.False(t, rs.IsEmpty())
	})
}

func TestInstRuleSet_String(t *testing.T) {
	rs := NewInstRuleSet("example.com/pkg")
	s := rs.String()
	assert.Contains(t, s, "example.com/pkg")
	assert.Contains(t, s, "value_decl=")
}

func TestInstRuleSet_AddRules(t *testing.T) {
	rs := NewInstRuleSet("example.com/pkg")
	file := filepath.Join(os.TempDir(), "file.go")

	rs.AddRawRule(file, &InstRawRule{InstBaseRule: InstBaseRule{Name: "raw"}})
	assert.Len(t, rs.RawRules[file], 1)

	rs.AddFuncRule(file, &InstFuncRule{InstBaseRule: InstBaseRule{Name: "func"}})
	assert.Len(t, rs.FuncRules[file], 1)

	rs.AddStructRule(file, &InstStructRule{InstBaseRule: InstBaseRule{Name: "struct"}})
	assert.Len(t, rs.StructRules[file], 1)

	rs.AddCallRule(file, &InstCallRule{InstBaseRule: InstBaseRule{Name: "call"}})
	assert.Len(t, rs.CallRules[file], 1)

	rs.AddDirectiveRule(file, &InstDirectiveRule{InstBaseRule: InstBaseRule{Name: "dir"}})
	assert.Len(t, rs.DirectiveRules[file], 1)

	rs.AddDeclRule(file, &InstDeclRule{InstBaseRule: InstBaseRule{Name: "decl"}})
	assert.Len(t, rs.DeclRules[file], 1)

	rs.AddValueDeclRule(file, &InstValueDeclRule{InstBaseRule: InstBaseRule{Name: "vdecl"}})
	assert.Len(t, rs.ValueDeclRules[file], 1)

	rs.AddFileRule(&InstFileRule{InstBaseRule: InstBaseRule{Name: "file"}})
	assert.Len(t, rs.FileRules, 1)
}

func TestInstRuleSet_SetPackageName(t *testing.T) {
	rs := NewInstRuleSet("example.com/pkg")
	rs.SetPackageName("mypkg")
	assert.Equal(t, "mypkg", rs.PackageName)
}

func TestInstRuleSet_SetCgoFileMap(t *testing.T) {
	rs := NewInstRuleSet("example.com/pkg")
	m := map[string]string{"a.go": "a_cgo.go"}
	rs.SetCgoFileMap(m)
	assert.Equal(t, m, rs.CgoFileMap)
}

func TestInstRuleSet_AllFuncRules(t *testing.T) {
	rs := NewInstRuleSet("example.com/pkg")
	f1 := filepath.Join(os.TempDir(), "f1.go")
	f2 := filepath.Join(os.TempDir(), "f2.go")

	r1 := &InstFuncRule{InstBaseRule: InstBaseRule{Name: "r1"}}
	r2 := &InstFuncRule{InstBaseRule: InstBaseRule{Name: "r2"}}
	rs.AddFuncRule(f1, r1)
	rs.AddFuncRule(f2, r2)

	all := rs.AllFuncRules()
	assert.Len(t, all, 2)
}

func TestInstRuleSet_AllStructRules(t *testing.T) {
	rs := NewInstRuleSet("example.com/pkg")
	f1 := filepath.Join(os.TempDir(), "f1.go")
	f2 := filepath.Join(os.TempDir(), "f2.go")

	r1 := &InstStructRule{InstBaseRule: InstBaseRule{Name: "s1"}}
	r2 := &InstStructRule{InstBaseRule: InstBaseRule{Name: "s2"}}
	rs.AddStructRule(f1, r1)
	rs.AddStructRule(f2, r2)

	all := rs.AllStructRules()
	assert.Len(t, all, 2)
}
