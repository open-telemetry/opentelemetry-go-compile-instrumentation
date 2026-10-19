// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVendoringActive(t *testing.T) {
	writeVendoredModule := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "vendor", "modules.txt"), []byte(""), 0o644))
		return root
	}

	t.Run("vendor present at module root", func(t *testing.T) {
		assert.True(t, vendoringActive(t.Context(), writeVendoredModule(t)))
	})
	t.Run("vendor found from subdirectory", func(t *testing.T) {
		root := writeVendoredModule(t)
		sub := filepath.Join(root, "cmd")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		assert.True(t, vendoringActive(t.Context(), sub))
	})
	t.Run("module without vendor", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
		assert.False(t, vendoringActive(t.Context(), root))
	})
	t.Run("no module", func(t *testing.T) {
		assert.False(t, vendoringActive(t.Context(), t.TempDir()))
	})
	t.Run("vendor present but in a workspace", func(t *testing.T) {
		root := writeVendoredModule(t)
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "go.work"),
			[]byte("go 1.25.0\n\nuse .\n"),
			0o644,
		))
		// In workspace mode the go command uses the vendor directory of the
		// workspace, not of the module, so a leftover vendor/modules.txt in the
		// module must not make this report true.
		assert.False(t, vendoringActive(t.Context(), root))
	})
}
