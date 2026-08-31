// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestValidateExcludeTargets(t *testing.T) {
	t.Parallel()

	require.NoError(t, rule.ValidateExcludeTargets(nil))
	require.NoError(t, rule.ValidateExcludeTargets([]string{"example.com/pkg"}))
	require.NoError(t, rule.ValidateExcludeTargets([]string{"example.com/svc/**"}))

	err := rule.ValidateExcludeTargets([]string{"example.com/svc/**", ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exclude_targets[1] is empty")

	err = rule.ValidateExcludeTargets([]string{"example.com/svc/**["})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exclude_targets[0]")
}

func TestMatchesExcludeTargets(t *testing.T) {
	t.Parallel()

	const importPath = "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

	assert.False(t, rule.MatchesExcludeTargets(importPath, nil))
	assert.False(t, rule.MatchesExcludeTargets(importPath, []string{"example.com/other"}))

	assert.True(t, rule.MatchesExcludeTargets(
		importPath,
		[]string{"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"},
	))
	assert.True(t, rule.MatchesExcludeTargets(
		importPath,
		[]string{"go.opentelemetry.io/contrib/instrumentation/**"},
	))
	assert.False(t, rule.MatchesExcludeTargets(
		importPath,
		[]string{"example.com/**"},
	))
}
