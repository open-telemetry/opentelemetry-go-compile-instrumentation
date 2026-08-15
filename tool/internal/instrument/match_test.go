// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func writeMatchedJSONForLoad(t *testing.T, contents string) {
	t.Helper()
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, util.BuildTempDir), 0o755))
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte(contents), 0o644))
}

func TestLoadAndMatchLegacyMatchedJSON(t *testing.T) {
	writeMatchedJSONForLoad(t, `[{
		"package_name":"svc",
		"module_path":"example.com/svc",
		"raw_rules":{},
		"func_rules":{
			"/abs/handler.go":[{
				"name":"hook-handler",
				"target":"example.com/svc",
				"func":"Handler",
				"before":"BeforeHandler",
				"path":"example.com/hooks"
			}]
		},
		"struct_rules":{},
		"call_rules":{},
		"directive_rules":{},
		"decl_rules":{},
		"file_rules":[]
	}]`)

	ip := &InstrumentPhase{logger: slog.Default()}
	sets, err := ip.load()
	require.NoError(t, err)
	require.Len(t, sets, 1)
	assert.Empty(t, sets[0].Version)
	assert.Nil(t, sets[0].Candidates)

	got := ip.match(sets, []string{"compile", "-p", "example.com/svc", "handler.go"})
	require.NotNil(t, got)
	require.Len(t, got.AllFuncRules(), 1)
	assert.Equal(t, "hook-handler", got.AllFuncRules()[0].Name)
	assert.Nil(t, ip.match(sets, []string{"compile", "-p", "example.com/other", "other.go"}))
}

func TestLoadAndMatchMatchedJSONWithCandidates(t *testing.T) {
	writeMatchedJSONForLoad(t, `[{
		"package_name":"svc",
		"module_path":"example.com/svc",
		"version":"v1.4.2",
		"candidates":{
			"func_rules":[{
				"name":"hook-handler",
				"target":"example.com/svc",
				"func":"Handler",
				"before":"BeforeHandler",
				"path":"example.com/hooks",
				"resolved_path":"/mod/hooks"
			}]
		},
		"raw_rules":{},
		"func_rules":{
			"/abs/handler.go":[{
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
	}]`)

	ip := &InstrumentPhase{logger: slog.Default()}
	sets, err := ip.load()
	require.NoError(t, err)
	require.Len(t, sets, 1)
	assert.Equal(t, "v1.4.2", sets[0].Version)
	require.NotNil(t, sets[0].Candidates)
	require.Len(t, sets[0].Candidates.FuncRules, 1)
	assert.Equal(t, "/mod/hooks", sets[0].Candidates.FuncRules[0].ResolvedPath)

	got := ip.match(sets, []string{"compile", "-p", "example.com/svc", "handler.go"})
	require.NotNil(t, got)
	require.Contains(t, got.FuncRules, "/abs/handler.go")
	assert.Equal(t, "hook-handler", got.AllFuncRules()[0].Name)
}
