// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	require.NoError(t, writeFileAtomic(path, []byte("[]\n")))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "[]\n", string(content))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestWriteFileAtomicMissingDirectory(t *testing.T) {
	err := writeFileAtomic(filepath.Join(t.TempDir(), "missing", "manifest.json"), []byte("[]\n"))
	require.Error(t, err)
}
