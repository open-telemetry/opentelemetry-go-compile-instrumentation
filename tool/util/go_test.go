// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCgoFile(t *testing.T) {
	tests := []struct {
		name       string
		cgoFile    string
		createFile string
		wantErr    bool
	}{
		{
			name:       "valid cgo file with source dir",
			cgoFile:    "$WORK/b001/main.cgo1.go",
			createFile: "main.go",
			wantErr:    false,
		},
		{
			name:       "valid cgo file in subdirectory",
			cgoFile:    "/tmp/work/subpkg/handler.cgo1.go",
			createFile: "handler.go",
			wantErr:    false,
		},
		{
			name:    "not a cgo file",
			cgoFile: "main.go",
			wantErr: true,
		},
		{
			name:    "cgo file but original does not exist in source dir",
			cgoFile: "missing.cgo1.go",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			if tt.createFile != "" {
				err := os.WriteFile(filepath.Join(tmpDir, tt.createFile), []byte("package main"), 0o644)
				require.NoError(t, err)
			}

			goFile, err := ResolveCgoFile(tt.cgoFile, tmpDir)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			expectedPath, err1 := filepath.EvalSymlinks(filepath.Join(tmpDir, tt.createFile))
			require.NoError(t, err1)
			gotPath, err2 := filepath.EvalSymlinks(goFile)
			require.NoError(t, err2)
			assert.Equal(t, expectedPath, gotPath)
		})
	}
}

func TestResolveCgoFile_EmptyParams(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("empty sourceDir returns error", func(t *testing.T) {
		_, err := ResolveCgoFile("server.cgo1.go", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty cgoFile returns error", func(t *testing.T) {
		_, err := ResolveCgoFile("", tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})
}
