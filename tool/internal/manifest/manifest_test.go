// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
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
	require.Len(t, slices.CompactFunc(slices.Clone(got), entriesEqual), len(got))
}

func TestLoadInvalidJSON(t *testing.T) {
	_, err := load([]byte("{"))
	require.ErrorContains(t, err, "loading embedded instrumentation manifest")
}

func compareEntries(a, b Entry) int {
	if cmp := strings.Compare(a.ModulePath, b.ModulePath); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(a.Target.String(), b.Target.String()); cmp != 0 {
		return cmp
	}
	return strings.Compare(a.VersionRange, b.VersionRange)
}

func entriesEqual(a, b Entry) bool {
	return a.ModulePath == b.ModulePath &&
		slices.Equal(a.Target.Include, b.Target.Include) &&
		slices.Equal(a.Target.Exclude, b.Target.Exclude) &&
		a.VersionRange == b.VersionRange
}

func TestLoadTargetList(t *testing.T) {
	got, err := load([]byte(`[
		{"modulePath":"example.com/instrumentation","target":["example.com/**",{"not":"example.com/mock"}]}
	]`))
	require.NoError(t, err)
	require.Equal(t, rule.Target{
		Include: []string{"example.com/**"},
		Exclude: []string{"example.com/mock"},
	}, got[0].Target)
}
