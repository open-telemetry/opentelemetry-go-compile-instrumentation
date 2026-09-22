// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// TestInjectedDependenciesAreMatched covers a rule whose target is only in the
// build because otelc put it there.
//
// The build plan comes from a dry run of the application as written. Matching
// that plan decides which hook packages otelc.runtime.go blank-imports, and
// those imports pull each hook package's own dependencies into the build. A
// rule targeting one of them therefore has nothing to match against unless
// matching runs again over what injection added.
//
// The injecteddeps application imports net/http and nothing else, so its
// instrumentation module's target package reaches the build only through the
// hook. Before the second pass, the application built and ran, the hook fired,
// and the flag stayed false with no diagnostic anywhere.
func TestInjectedDependenciesAreMatched(t *testing.T) {
	t.Parallel()

	const targetPkg = util.OtelcRoot + "/test/apps/injecteddeps/instrumentation/target"

	testutil.Build(t, "", "injecteddeps", "go", "build", "-a")

	matchedPath := filepath.Join("../", "apps", "injecteddeps", ".otelc-build", "matched.json")
	require.FileExists(t, matchedPath)

	raw, err := os.ReadFile(matchedPath)
	require.NoError(t, err)

	var matched []struct {
		ModulePath string `json:"module_path"`
	}
	require.NoError(t, json.Unmarshal(raw, &matched),
		"matched.json is null when matching never ran; it must be a list here")

	modules := make([]string, 0, len(matched))
	for _, m := range matched {
		modules = append(modules, m.ModulePath)
	}
	assert.Contains(t, modules, "net/http",
		"the hook that pulls the target package into the build must still match")
	assert.Contains(t, modules, targetPkg,
		"a rule targeting a package only otelc adds to the build must be matched")

	// The flag is false in the source and is flipped by the rule above, so the
	// output distinguishes "rule applied" from "package compiled uninstrumented".
	// The hook printing it proves instrumentation ran at all, which keeps a
	// wholly uninstrumented build from passing this test quietly.
	output := testutil.Run(t, "", "injecteddeps", nil)
	assert.Contains(t, output, "injecteddeps: target.Instrumented=true")
	assert.Contains(t, output, "injecteddeps: done")
}
