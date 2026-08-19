// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstRuleSetJSONRoundTripCandidates(t *testing.T) {
	absFile := filepath.Join(t.TempDir(), "handler.go")
	funcRule := &InstFuncRule{
		InstBaseRule: InstBaseRule{
			Name:    "hook-handler",
			Target:  "example.com/svc",
			Version: "v1.0.0,v2.0.0",
		},
		Func:         "Handler",
		Before:       "BeforeHandler",
		Path:         "example.com/hooks",
		ResolvedPath: "/mod/hooks",
	}
	fileRule := &InstFileRule{
		InstBaseRule: InstBaseRule{Name: "add-file", Target: "example.com/svc"},
		File:         "extra.go",
		Path:         "example.com/hooks",
		ResolvedPath: "/mod/hooks",
	}

	original := NewInstRuleSet("example.com/svc")
	original.PackageName = "svc"
	original.Version = "v1.4.2"
	original.AddFuncRule(absFile, funcRule)
	original.AddFileRule(fileRule)
	original.SetCandidates([]InstRule{funcRule, fileRule})

	raw, err := json.Marshal([]*InstRuleSet{original})
	require.NoError(t, err)

	var decoded []*InstRuleSet
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Len(t, decoded, 1)

	got := decoded[0]
	assert.Equal(t, "svc", got.PackageName)
	assert.Equal(t, "example.com/svc", got.ModulePath)
	assert.Equal(t, "v1.4.2", got.Version)
	require.NotNil(t, got.Candidates)
	require.Len(t, got.Candidates.FuncRules, 1)
	require.Len(t, got.Candidates.FileRules, 1)
	assert.Equal(t, "hook-handler", got.Candidates.FuncRules[0].Name)
	assert.Equal(t, "/mod/hooks", got.Candidates.FuncRules[0].ResolvedPath)
	assert.Equal(t, "add-file", got.Candidates.FileRules[0].Name)
	assert.Equal(t, "/mod/hooks", got.Candidates.FileRules[0].ResolvedPath)

	require.Contains(t, got.FuncRules, absFile)
	require.Len(t, got.FuncRules[absFile], 1)
	assert.Equal(t, "hook-handler", got.FuncRules[absFile][0].Name)
	require.Len(t, got.FileRules, 1)
	assert.Equal(t, "add-file", got.FileRules[0].Name)
}

func TestInstRuleSetJSONLegacyWithoutCandidates(t *testing.T) {
	const legacy = `[{
		"package_name":"svc",
		"module_path":"example.com/svc",
		"raw_rules":{},
		"func_rules":{
			"/abs/pkg/handler.go":[{
				"name":"hook-handler",
				"target":"example.com/svc",
				"func":"Handler",
				"before":"BeforeHandler",
				"path":"example.com/hooks",
				"resolved_path":"/mod/hooks"
			}]
		},
		"struct_rules":{},
		"call_rules":{},
		"directive_rules":{},
		"decl_rules":{},
		"file_rules":[]
	}]`

	var decoded []*InstRuleSet
	require.NoError(t, json.Unmarshal([]byte(legacy), &decoded))
	require.Len(t, decoded, 1)
	assert.Empty(t, decoded[0].Version)
	assert.Nil(t, decoded[0].Candidates)
	require.Len(t, decoded[0].AllFuncRules(), 1)
	assert.Equal(t, "hook-handler", decoded[0].AllFuncRules()[0].Name)
}

func TestSetCandidatesEmptyClearsField(t *testing.T) {
	set := NewInstRuleSet("example.com/svc")
	set.SetCandidates([]InstRule{
		&InstFuncRule{
			InstBaseRule: InstBaseRule{Name: "r", Target: "example.com/svc"},
			Func:         "F",
			Before:       "B",
			Path:         "example.com/hooks",
		},
	})
	require.NotNil(t, set.Candidates)

	set.SetCandidates(nil)
	assert.Nil(t, set.Candidates)
}
