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

func TestResolveCgoSourceFile(t *testing.T) {
	tests := []struct {
		name        string
		cgoFile     string
		createFile  string
		wantErr     bool
		wantErrType error
	}{
		{
			name:       "valid cgo file with existing original",
			cgoFile:    "main.cgo1.go",
			createFile: "main.go",
			wantErr:    false,
		},
		{
			name:       "valid cgo file with path prefix",
			cgoFile:    "/some/path/handler.cgo1.go",
			createFile: "handler.go",
			wantErr:    false,
		},
		{
			name:    "not a cgo file - regular go file",
			cgoFile: "main.go",
			wantErr: true,
		},
		{
			name:    "not a cgo file - wrong suffix",
			cgoFile: "main.cgo2.go",
			wantErr: true,
		},
		{
			name:    "cgo file but original does not exist",
			cgoFile: "missing.cgo1.go",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Chdir(tmpDir)
			if tt.createFile != "" {
				err := os.WriteFile(tt.createFile, []byte("package main"), 0o644)
				require.NoError(t, err)
			}

			goFile, err := ResolveCgoFile(tt.cgoFile)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrType != nil {
					require.ErrorIs(t, err, tt.wantErrType)
				}
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
