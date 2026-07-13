// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverWorkDir(t *testing.T) {
	newDir := func(t *testing.T, parts ...string) string {
		t.Helper()
		dir := filepath.Join(parts...)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	touch := func(t *testing.T, path string) {
		t.Helper()
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("found in current directory", func(t *testing.T) {
		module := newDir(t, t.TempDir(), "module")
		newDir(t, module, BuildTempDir)
		touch(t, filepath.Join(module, "go.mod"))

		if got := DiscoverWorkDir(module); got != module {
			t.Errorf("DiscoverWorkDir() = %q, want %q", got, module)
		}
	})

	t.Run("found walking up from a subdirectory", func(t *testing.T) {
		module := newDir(t, t.TempDir(), "module")
		newDir(t, module, BuildTempDir)
		touch(t, filepath.Join(module, "go.mod"))
		sub := newDir(t, module, "internal", "app")

		if got := DiscoverWorkDir(sub); got != module {
			t.Errorf("DiscoverWorkDir() = %q, want %q", got, module)
		}
	})

	t.Run("stops at go.mod when no work dir exists", func(t *testing.T) {
		// .otelc-build exists above the module boundary and must not be found.
		root := t.TempDir()
		newDir(t, root, BuildTempDir)
		module := newDir(t, root, "module")
		touch(t, filepath.Join(module, "go.mod"))
		sub := newDir(t, module, "internal")

		if got := DiscoverWorkDir(sub); got != "" {
			t.Errorf("DiscoverWorkDir() = %q, want empty", got)
		}
	})
}

func TestStripToolexecFromGoflags(t *testing.T) {
	tests := []struct {
		name     string
		goflags  string
		expected string
	}{
		{
			name:     "empty",
			goflags:  "",
			expected: "",
		},
		{
			name:     "no toolexec flag",
			goflags:  "-mod=mod -race",
			expected: "-mod=mod -race",
		},
		{
			name:     "bare toolexec flag",
			goflags:  "-toolexec=otelc",
			expected: "",
		},
		{
			name:     "single-quoted toolexec flag with space",
			goflags:  "'-toolexec=otelc toolexec'",
			expected: "",
		},
		{
			name:     "double-quoted toolexec flag with space",
			goflags:  `"-toolexec=otelc toolexec"`,
			expected: "",
		},
		{
			name:     "toolexec flag between other flags",
			goflags:  "-mod=mod '-toolexec=otelc toolexec' -race",
			expected: "-mod=mod -race",
		},
		{
			name:     "other quoted flags are preserved verbatim",
			goflags:  "'-tags=a b' -toolexec=otelc",
			expected: "'-tags=a b'",
		},
		{
			name:     "extra whitespace between flags",
			goflags:  "  -mod=mod   -toolexec=otelc  ",
			expected: "-mod=mod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripToolexecFromGoflags(tt.goflags); got != tt.expected {
				t.Errorf("StripToolexecFromGoflags(%q) = %q, want %q", tt.goflags, got, tt.expected)
			}
		})
	}
}
