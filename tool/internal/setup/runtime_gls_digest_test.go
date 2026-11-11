// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/instrument"
	toolruntime "go.opentelemetry.io/otelc/tool/internal/runtime"
	"go.opentelemetry.io/otelc/tool/util"
)

// TestRuntimeGLSDigestIsWorkDirIndependent covers builds that run the same
// setup with different --work-dir values over one GOCACHE. The runtime GLS
// source setup generates must be identical in each work dir, and the rules
// digest that feeds otelc's tool ID must not depend on where it was written,
// or no build could reuse another's cached compiles.
func TestRuntimeGLSDigestIsWorkDirIndependent(t *testing.T) {
	runtimeSrc := t.TempDir()
	deps := []*Dependency{{
		ImportPath: toolruntime.TargetRuntime,
		Sources: []string{
			filepath.Join(runtimeSrc, "runtime2.go"),
			filepath.Join(runtimeSrc, "proc.go"),
		},
	}}

	type result struct {
		source []byte
		digest string
		dir    string
	}
	setupIn := func(workDir string) result {
		t.Helper()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		sets, err := matchRuntimeGLS(deps)
		require.NoError(t, err)
		require.NotEmpty(t, sets)
		matched, err := json.Marshal(sets)
		require.NoError(t, err)
		dir := util.GetBuildTemp("runtime")
		require.Contains(t, string(matched), filepath.Base(filepath.Dir(dir)),
			"the matched rules reference the generated runtime source")
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), matched, 0o644))
		source, err := os.ReadFile(filepath.Join(dir, toolruntime.GLSFileName))
		require.NoError(t, err)
		return result{source: source, digest: instrument.RulesDigest(), dir: dir}
	}

	first := setupIn(t.TempDir())
	second := setupIn(t.TempDir())

	require.NotEqual(t, first.dir, second.dir)
	assert.Equal(t, first.source, second.source,
		"the generated runtime GLS source must be identical in every work dir")
	assert.NotEmpty(t, first.digest)
	assert.Equal(t, first.digest, second.digest,
		"builds in different work dirs must report the same tool ID")
}
