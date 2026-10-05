// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnresolvedVersionSkip(t *testing.T) {
	assert.True(t, unresolvedVersionSkip("", "v1.0.0"))
	assert.True(t, unresolvedVersionSkip("", "v1.0.0,v2.0.0"))
	assert.False(t, unresolvedVersionSkip("v1.0.0", "v1.0.0"))
	assert.False(t, unresolvedVersionSkip("", ""))
	assert.False(t, unresolvedVersionSkip("v1.0.0", ""))
}

func TestWarnUnresolvedVersionSkip(t *testing.T) {
	var gotMsg string
	var gotArgs []any
	warnUnresolvedVersionSkip(func(msg string, args ...any) {
		gotMsg = msg
		gotArgs = args
	}, "example.com/foo", "v1.0.0", "rule", "foo_hook")

	require.Equal(t, unresolvedVersionSkipMsg, gotMsg)
	require.Equal(t, []any{"dep", "example.com/foo", "version_range", "v1.0.0", "rule", "foo_hook"}, gotArgs)

	assert.NotPanics(t, func() {
		warnUnresolvedVersionSkip(nil, "example.com/foo", "v1.0.0")
	})
}
