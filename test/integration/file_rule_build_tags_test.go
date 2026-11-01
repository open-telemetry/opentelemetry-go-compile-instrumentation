//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestFileRuleRespectsBuildTags checks that an add_file rule whose file is
// guarded by a custom build tag is applied only when the build passes that tag.
func TestFileRuleRespectsBuildTags(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-buildtags

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, "answer.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package buildtags

import "fmt"

func Answer() string {
	return fmt.Sprint(42)
}
`)
	writeTestFile(t, moduleDir, "answer_test.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package buildtags

import "testing"

func TestAnswer(t *testing.T) {
	if got := Answer(); got != "42" {
		t.Fatalf("Answer() = %q, want 42", got)
	}
}
`)
	// The stub keeps the package loadable; the rule file itself is build-ignored.
	writeTestFile(t, moduleDir, filepath.Join("extra", "doc.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package extra
`)
	writeTestFile(t, moduleDir, filepath.Join("extra", "enterprise.go"), `//go:build ignore && enterprise

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package extra

import "fmt"

func init() {
	fmt.Println("enterprise file added")
}
`)
	writeTestFile(t, moduleDir, "rules.yml", `add_enterprise_file:
  target: example.com/otelc-buildtags
  do:
    - add_file:
        file: enterprise.go
        path: example.com/otelc-buildtags/extra
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")

	// The untagged build runs second so it would reuse the tagged build's cached
	// package if the build tags were not part of the cache key.

	t.Run("with tag", func(t *testing.T) {
		output := runOtelcCommand(t, moduleDir, env, otelcPath,
			"go", "test", "-count=1", "-v", "-tags=enterprise", ".")
		require.Contains(t, output, "enterprise file added")
	})

	t.Run("without tag", func(t *testing.T) {
		output := runOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "-v", ".")
		require.NotContains(t, output, "enterprise file added")
	})
}
