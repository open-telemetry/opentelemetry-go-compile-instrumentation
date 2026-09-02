// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithSourceRoot(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv("OTELC_SOURCE_ROOT", "")
		env := []string{"PATH=/bin"}
		require.Equal(t, env, withSourceRoot(env))
	})

	t.Run("inherited", func(t *testing.T) {
		t.Setenv("OTELC_SOURCE_ROOT", "/repo")
		require.Equal(t, []string{"PATH=/bin", "OTELC_SOURCE_ROOT=/repo"}, withSourceRoot([]string{"PATH=/bin"}))
	})

	t.Run("explicit override", func(t *testing.T) {
		t.Setenv("OTELC_SOURCE_ROOT", "/repo")
		env := []string{"PATH=/bin", "OTELC_SOURCE_ROOT=/override"}
		require.Equal(t, env, withSourceRoot(env))
	})
}
