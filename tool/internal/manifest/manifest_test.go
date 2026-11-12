// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	got, err := Load()
	require.NoError(t, err)
	require.NotEmpty(t, got)
	for _, entry := range got {
		require.NotEmpty(t, entry.ModulePath)
		require.NotEmpty(t, entry.Target)
	}
	require.True(t, slices.IsSortedFunc(got, compareEntries))
	require.Len(t, slices.Compact(slices.Clone(got)), len(got))
}

func TestLoadInvalidJSON(t *testing.T) {
	_, err := load([]byte("{"))
	require.ErrorContains(t, err, "loading embedded instrumentation manifest")
}

func compareEntries(a, b Entry) int {
	if cmp := strings.Compare(a.ModulePath, b.ModulePath); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(a.Target, b.Target); cmp != 0 {
		return cmp
	}
	return strings.Compare(a.VersionRange, b.VersionRange)
}
