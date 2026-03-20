// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "strips go:build ignore comment",
			input: `//go:build ignore

package main

func main() {}
`,
			expected: `

package main

func main() {}
`,
		},
		{
			name: "no go:build ignore comment - content unchanged",
			input: `package main

func main() {}
`,
			expected: `package main

func main() {}
`,
		},
		{
			name: "multiple go:build ignore comments",
			input: `//go:build ignore
package main
//go:build ignore
func main() {}
`,
			expected: `
package main

func main() {}
`,
		},
		{
			name:     "empty file",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "test.go")
			err := os.WriteFile(file, []byte(tt.input), 0o644)
			require.NoError(t, err)

			err = stripComments(file)
			require.NoError(t, err)

			data, err := os.ReadFile(file)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(data))
		})
	}
}

func TestStripComments_NonexistentFile(t *testing.T) {
	// WriteFile will fail when the directory does not exist.
	err := stripComments("/nonexistent/path/file.go")
	assert.Error(t, err)
}
