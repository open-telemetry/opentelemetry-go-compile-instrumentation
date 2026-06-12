// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLowerBounds(t *testing.T) {
	tests := []struct {
		name       string
		rangedDeps map[string][]string
		want       map[string]string
	}{
		{
			name:       "no deps",
			rangedDeps: map[string][]string{},
			want:       map[string]string{},
		},
		{
			name:       "open-ended range",
			rangedDeps: map[string][]string{"go.opentelemetry.io/otel": {"v1.39.0"}},
			want:       map[string]string{"go.opentelemetry.io/otel": "v1.39.0"},
		},
		{
			name:       "half-open range",
			rangedDeps: map[string][]string{"k8s.io/client-go": {"v0.34.0,v0.36.0"}},
			want:       map[string]string{"k8s.io/client-go": "v0.34.0"},
		},
		{
			name: "lowest across multiple ranges",
			rangedDeps: map[string][]string{
				"k8s.io/client-go": {"v0.34.0,v0.36.0", "v0.35.0,v0.36.0"},
			},
			want: map[string]string{"k8s.io/client-go": "v0.34.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, LowerBounds(tt.rangedDeps))
		})
	}
}

func TestHighestCovered(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		ranges   []string
		want     string
	}{
		{
			name:     "newest release below the cap",
			versions: []string{"v0.33.0", "v0.34.0", "v0.35.0", "v0.35.5", "v0.36.0", "v0.36.1"},
			ranges:   []string{"v0.34.0,v0.36.0"},
			want:     "v0.35.5",
		},
		{
			name:     "cap itself is excluded",
			versions: []string{"v0.34.0", "v0.36.0"},
			ranges:   []string{"v0.34.0,v0.36.0"},
			want:     "v0.34.0",
		},
		{
			name:     "open-ended range takes the latest release",
			versions: []string{"v1.39.0", "v1.42.0", "v1.43.0"},
			ranges:   []string{"v1.39.0"},
			want:     "v1.43.0",
		},
		{
			name:     "prereleases are skipped",
			versions: []string{"v0.35.5", "v0.36.0-alpha.1", "v0.36.0-rc.1"},
			ranges:   []string{"v0.34.0,v0.36.0"},
			want:     "v0.35.5",
		},
		{
			name:     "highest across multiple ranges",
			versions: []string{"v0.34.0", "v0.35.0", "v0.35.5"},
			ranges:   []string{"v0.34.0,v0.35.0", "v0.35.0,v0.36.0"},
			want:     "v0.35.5",
		},
		{
			name:     "no published release covered",
			versions: []string{"v0.33.0", "v0.36.0"},
			ranges:   []string{"v0.34.0,v0.36.0"},
			want:     "",
		},
		{
			name:     "unsorted version list",
			versions: []string{"v0.35.0", "v0.34.0", "v0.35.5"},
			ranges:   []string{"v0.34.0,v0.36.0"},
			want:     "v0.35.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, highestCovered(tt.versions, tt.ranges))
		})
	}
}

func TestDiscoverRangedDeps(t *testing.T) {
	appDir := t.TempDir()
	goMod := `module example.com/app

go 1.25.0

require (
	github.com/redis/go-redis/v9 v9.0.0
	go.opentelemetry.io/otel v1.43.0
	k8s.io/client-go v0.34.0
	example.com/unused v1.0.0 // indirect
	example.com/local v1.0.0
)

replace example.com/local => ./local
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(goMod), 0o600))

	targets := map[string][]string{
		// Empty range: nothing to verify, so the dep is not part of the matrix.
		"github.com/redis/go-redis/v9":       {""},
		"go.opentelemetry.io/otel/sdk/trace": {"v1.39.0"},
		// Capped ranges must be discovered regardless of where @latest falls,
		// unlike DiscoverInstrumentedDeps.
		"k8s.io/client-go/tools/cache": {"v0.34.0,v0.36.0", "v0.35.0,v0.36.0"},
		"example.com/unused":           {"v1.0.0"},
		// The local replacement should be ignored even though it is covered by a target.
		"example.com/local": {"v1.0.0"},
	}

	deps := DiscoverRangedDeps(t, appDir, targets)
	require.Equal(t, map[string][]string{
		"go.opentelemetry.io/otel": {"v1.39.0"},
		"k8s.io/client-go":         {"v0.34.0,v0.36.0", "v0.35.0,v0.36.0"},
	}, deps)
}
