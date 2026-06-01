// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindMatchingVersionRanges(t *testing.T) {
	tests := []struct {
		name        string
		requirePath string
		targets     map[string][]string
		want        []string
	}{
		{
			name:        "exact match",
			requirePath: "github.com/redis/go-redis/v9",
			targets: map[string][]string{
				"github.com/redis/go-redis/v9": []string{"v9.0.0,v10.0.0"},
			},
			want: []string{"v9.0.0,v10.0.0"},
		},
		{
			name:        "module covers subpackage target",
			requirePath: "go.opentelemetry.io/otel",
			targets: map[string][]string{
				"go.opentelemetry.io/otel/sdk/trace": []string{"v1.0.0"},
			},
			want: []string{"v1.0.0"},
		},
		{
			name:        "multiple targets covered",
			requirePath: "k8s.io/client-go",
			targets: map[string][]string{
				"k8s.io/client-go":                   []string{"v0.34.0,v0.35.0", "v0.35.0,v0.36.0"},
				"k8s.io/client-go/tools/portforward": []string{"v0.35.0"},
			},
			want: []string{"v0.34.0,v0.35.0", "v0.35.0,v0.36.0", "v0.35.0"},
		},
		{
			name:        "prefix false positive",
			requirePath: "example.com/foo",
			targets: map[string][]string{
				"example.com/foobar": []string{"v1.0.0"},
			},
			want: []string{},
		},
		{
			name:        "unrelated target",
			requirePath: "google.golang.org/grpc",
			targets: map[string][]string{
				"net/http": []string{"v1.0.0"},
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findMatchingVersionRanges(tt.requirePath, tt.targets)
			require.Equal(t, len(tt.want), len(got))
			for _, w := range tt.want {
				require.Contains(t, got, w)
			}
		})
	}
}

func TestCoversAnyVersionRange(t *testing.T) {
	tests := []struct {
		name          string
		version       string
		versionRanges map[string]bool
		want          bool
	}{
		{
			name:    "version in range",
			version: "v1.5.0",
			versionRanges: map[string]bool{
				"v1.0.0,v2.0.0": true,
			},
			want: true,
		},
		{
			name:    "version equals lower range boundary",
			version: "v1.0.0",
			versionRanges: map[string]bool{
				"v1.0.0,v2.0.0": true,
			},
			want: true,
		},
		{
			name:    "version equals upper range boundary",
			version: "v2.0.0",
			versionRanges: map[string]bool{
				"v1.0.0,v2.0.0": true,
			},
			want: false, // upper boundary is exclusive
		},
		{
			name:    "version below range",
			version: "v0.9.9",
			versionRanges: map[string]bool{
				"v1.0.0,v2.0.0": true,
			},
			want: false,
		},
		{
			name:    "version above range",
			version: "v2.0.1",
			versionRanges: map[string]bool{
				"v1.0.0,v2.0.0": true,
			},
			want: false,
		},
		{
			name:    "multiple ranges with one match",
			version: "v1.5.0",
			versionRanges: map[string]bool{
				"v1.0.0,v1.4.0": true,
				"v1.5.0,v2.0.0": true,
			},
			want: true,
		},
		{
			name:    "multiple ranges with no matches",
			version: "v1.5.0",
			versionRanges: map[string]bool{
				"v1.0.0,v1.4.0": true,
				"v1.6.0,v2.0.0": true,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, coversAnyVersionRange(tt.version, tt.versionRanges))
		})
	}
}

func TestDiscoverInstrumentedDeps(t *testing.T) {
	appDir := t.TempDir()
	goMod := `module example.com/app

go 1.25.0

require (
	github.com/redis/go-redis/v9 v9.0.0
	go.opentelemetry.io/otel v1.0.0
	k8s.io/client-go v0.35.0
	example.com/unused v1.0.0 // indirect
	example.com/local v1.0.0
)

replace example.com/local => ./local
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(goMod), 0o600))

	targets := map[string][]string{
		"github.com/redis/go-redis/v9":       []string{"v9.0.0,v10.0.0"},
		"go.opentelemetry.io/otel/sdk/trace": []string{"v1.0.0"},
		"k8s.io/client-go":                   []string{"v0.34.0,v0.35.0", "v0.35.0,v0.36.0"},
		"example.com/unused":                 []string{""},
		// The local replacement should be ignored even though it is covered by a target.
		"example.com/local": []string{""},
	}

	// k8s.io/client-go should be skipped because its latest version exceeds the specified version ranges.
	deps := DiscoverInstrumentedDeps(t, appDir, targets)
	require.ElementsMatch(t, []string{
		"github.com/redis/go-redis/v9",
		"go.opentelemetry.io/otel",
	}, deps)
}
