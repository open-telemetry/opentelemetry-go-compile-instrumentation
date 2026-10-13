// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeModuleDirs(t *testing.T) {
	t.Run("empty slice returns empty non-nil slice", func(t *testing.T) {
		got := normalizeModuleDirs(nil)
		assert.NotNil(t, got)
		assert.Empty(t, got)

		got = normalizeModuleDirs([]string{})
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("sorts and compacts without mutating caller slice", func(t *testing.T) {
		dirs := []string{"/dir/c", "/dir/a", "/dir/b", "/dir/a", "/dir/c"}
		dirsOrig := append([]string(nil), dirs...)

		got := normalizeModuleDirs(dirs)
		assert.Equal(t, []string{"/dir/a", "/dir/b", "/dir/c"}, got)
		assert.Equal(t, dirsOrig, dirs, "caller slice must not be mutated")
	})
}
