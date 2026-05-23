// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCoversAnyTarget(t *testing.T) {
	tests := []struct {
		name        string
		requirePath string
		targets     map[string]bool
		want        bool
	}{
		{
			name:        "exact match",
			requirePath: "github.com/redis/go-redis/v9",
			targets: map[string]bool{
				"github.com/redis/go-redis/v9": true,
			},
			want: true,
		},
		{
			name:        "module covers subpackage target",
			requirePath: "go.opentelemetry.io/otel",
			targets: map[string]bool{
				"go.opentelemetry.io/otel/sdk/trace": true,
			},
			want: true,
		},
		{
			name:        "prefix false positive",
			requirePath: "example.com/foo",
			targets: map[string]bool{
				"example.com/foobar": true,
			},
			want: false,
		},
		{
			name:        "unrelated target",
			requirePath: "google.golang.org/grpc",
			targets: map[string]bool{
				"net/http": true,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, coversAnyTarget(tt.requirePath, tt.targets))
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
	example.com/unused v1.0.0 // indirect
	example.com/local v1.0.0
)

replace example.com/local => ./local
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(goMod), 0o600))

	targets := map[string]bool{
		"github.com/redis/go-redis/v9":       true,
		"go.opentelemetry.io/otel/sdk/trace": true,
		"example.com/unused":                 true,
		// The local replacement should be ignored even though it is covered by a target.
		"example.com/local": true,
	}

	deps := DiscoverInstrumentedDeps(t, appDir, targets)
	require.ElementsMatch(t, []string{
		"github.com/redis/go-redis/v9",
		"go.opentelemetry.io/otel",
	}, deps)
}
