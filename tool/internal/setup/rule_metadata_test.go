// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"

	instrule "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func TestInstrumentationRuleMetadata(t *testing.T) {
	root := filepath.Join("..", "..", "..", "pkg", "instrumentation")

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files, "expected instrumentation rule YAML files")

	for _, file := range files {
		name, relErr := filepath.Rel(root, file)
		require.NoError(t, relErr)

		t.Run(name, func(t *testing.T) {
			content, readErr := os.ReadFile(file)
			require.NoError(t, readErr)

			rules, parseErr := parseRuleFromYaml(content)
			require.NoError(t, parseErr)
			require.NotEmpty(t, rules, "%s: expected at least one rule", file)

			for _, parsedRule := range rules {
				ruleName := parsedRule.GetName()
				require.NotEmpty(t, ruleName, "%s: rule name is empty", file)
				require.NotEmpty(t, parsedRule.GetTarget(), "%s: rule %q target is empty", file, ruleName)

				validateRuleVersion(t, file, ruleName, parsedRule.GetVersion())

				switch rule := parsedRule.(type) {
				case *instrule.InstFuncRule:
					require.NotEmpty(t, rule.Path, "%s: rule %q path is empty", file, ruleName)
				case *instrule.InstFileRule:
					require.NotEmpty(t, rule.Path, "%s: rule %q path is empty", file, ruleName)
				}
			}
		})
	}
}

func validateRuleVersion(t *testing.T, file, ruleName, version string) {
	t.Helper()

	if version == "" {
		return
	}

	parts := strings.Split(version, ",")
	require.LessOrEqual(t,
		len(parts),
		2,
		"%s: rule %q has invalid version range %q",
		file,
		ruleName,
		version,
	)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		require.NotEmpty(t, part, "%s: rule %q has empty version component in %q", file, ruleName, version)
		require.True(t, semver.IsValid(part), "%s: rule %q has invalid semver version %q", file, ruleName, part)
	}
}
