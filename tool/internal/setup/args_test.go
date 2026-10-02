// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyArgs_Build(t *testing.T) {
	t.Run("flags, separated values, and targets", func(t *testing.T) {
		args := []string{"-o", "out", "-tags", "foo", "./pkg"}
		got := classifyArgs(subcmdBuild, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-o", Kind: ArgBuildFlag, FlagName: "-o", HasValue: false},
			{Index: 1, Raw: "out", Kind: ArgBuildFlagValue, FlagName: "-o", Value: "out"},
			{Index: 2, Raw: "-tags", Kind: ArgBuildFlag, FlagName: "-tags", HasValue: false},
			{Index: 3, Raw: "foo", Kind: ArgBuildFlagValue, FlagName: "-tags", Value: "foo"},
			{Index: 4, Raw: "./pkg", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("joined values and booleans", func(t *testing.T) {
		args := []string{"--tags=foo", "-race", "--race=false", "main.go"}
		got := classifyArgs(subcmdBuild, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "--tags=foo", Kind: ArgBuildFlag, FlagName: "-tags", HasValue: true, Value: "foo"},
			{Index: 1, Raw: "-race", Kind: ArgBuildFlag, FlagName: "-race", HasValue: false},
			{Index: 2, Raw: "--race=false", Kind: ArgBuildFlag, FlagName: "-race", HasValue: true, Value: "false"},
			{Index: 3, Raw: "main.go", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("terminator closes flag parsing and treats dash-prefixed token as target", func(t *testing.T) {
		args := []string{"-v", "--", "-weird-target", "./pkg"}
		got := classifyArgs(subcmdBuild, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-v", Kind: ArgBuildFlag, FlagName: "-v", HasValue: false},
			{Index: 1, Raw: "--", Kind: ArgFlagTerminator},
			{Index: 2, Raw: "-weird-target", Kind: ArgTarget},
			{Index: 3, Raw: "./pkg", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("install behaves like build", func(t *testing.T) {
		args := []string{"-v", "./..."}
		got := classifyArgs(subcmdInstall, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-v", Kind: ArgBuildFlag, FlagName: "-v", HasValue: false},
			{Index: 1, Raw: "./...", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})
}

func TestClassifyArgs_Test_Delimiters(t *testing.T) {
	t.Run("dash-dash delimiter terminates into test binary args", func(t *testing.T) {
		args := []string{"./pkg", "--", "-mod=vendor", "-tags=integration"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "--", Kind: ArgTestDelimiter},
			{Index: 2, Raw: "-mod=vendor", Kind: ArgTestBinary},
			{Index: 3, Raw: "-tags=integration", Kind: ArgTestBinary},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("-args delimiter", func(t *testing.T) {
		args := []string{"./pkg", "-args", "-custom", "value"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "-args", Kind: ArgTestDelimiter},
			{Index: 2, Raw: "-custom", Kind: ArgTestBinary},
			{Index: 3, Raw: "value", Kind: ArgTestBinary},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("--args delimiter", func(t *testing.T) {
		args := []string{"./pkg", "--args", "-custom"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "--args", Kind: ArgTestDelimiter},
			{Index: 2, Raw: "-custom", Kind: ArgTestBinary},
		}
		assert.Equal(t, expected, got)
	})
}

func TestClassifyArgs_Test_ValuesResemblingFlagsOrDelimiters(t *testing.T) {
	t.Run("test flag value is dash-dash delimiter", func(t *testing.T) {
		args := []string{"-test.run", "--", "./pkg"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-test.run", Kind: ArgTestFlag, FlagName: "-test.run", HasValue: false},
			{Index: 1, Raw: "--", Kind: ArgTestFlagValue, FlagName: "-test.run", Value: "--"},
			{Index: 2, Raw: "./pkg", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("test flag value is -args delimiter", func(t *testing.T) {
		args := []string{"-test.run", "-args", "./pkg"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-test.run", Kind: ArgTestFlag, FlagName: "-test.run", HasValue: false},
			{Index: 1, Raw: "-args", Kind: ArgTestFlagValue, FlagName: "-test.run", Value: "-args"},
			{Index: 2, Raw: "./pkg", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("test flag value is --args delimiter", func(t *testing.T) {
		args := []string{"-run", "--args", "./pkg"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-run", Kind: ArgTestFlag, FlagName: "-run", HasValue: false},
			{Index: 1, Raw: "--args", Kind: ArgTestFlagValue, FlagName: "-run", Value: "--args"},
			{Index: 2, Raw: "./pkg", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("test flag value looks like build flag", func(t *testing.T) {
		args := []string{"-test.run", "-mod=vendor", "./pkg"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-test.run", Kind: ArgTestFlag, FlagName: "-test.run", HasValue: false},
			{Index: 1, Raw: "-mod=vendor", Kind: ArgTestFlagValue, FlagName: "-test.run", Value: "-mod=vendor"},
			{Index: 2, Raw: "./pkg", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("test flag value looks like tags build flag", func(t *testing.T) {
		args := []string{"-run", "-tags=integration", "./pkg"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-run", Kind: ArgTestFlag, FlagName: "-run", HasValue: false},
			{Index: 1, Raw: "-tags=integration", Kind: ArgTestFlagValue, FlagName: "-run", Value: "-tags=integration"},
			{Index: 2, Raw: "./pkg", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})
}

func TestClassifyArgs_Test_UnknownFlags(t *testing.T) {
	t.Run("unknown flag with separated value terminates package discovery", func(t *testing.T) {
		args := []string{"./pkg", "-custom", "value"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "-custom", Kind: ArgTestBinary, FlagName: "-custom", HasValue: false},
			{Index: 2, Raw: "value", Kind: ArgTestBinary, FlagName: "-custom", Value: "value"},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("unknown flag before package closes package list", func(t *testing.T) {
		args := []string{"-custom", "value", "./pkg"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-custom", Kind: ArgTestBinary, FlagName: "-custom", HasValue: false},
			{Index: 1, Raw: "value", Kind: ArgTestBinary, FlagName: "-custom", Value: "value"},
			{Index: 2, Raw: "./pkg", Kind: ArgTestBinary},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("unknown flag with joined value", func(t *testing.T) {
		args := []string{"./pkg", "-custom=value"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "-custom=value", Kind: ArgTestBinary, FlagName: "-custom", HasValue: true, Value: "value"},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("unknown flag followed by delimiter", func(t *testing.T) {
		args := []string{"./pkg", "-custom", "--", "./other"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "-custom", Kind: ArgTestBinary, FlagName: "-custom", HasValue: false},
			{Index: 2, Raw: "--", Kind: ArgTestDelimiter},
			{Index: 3, Raw: "./other", Kind: ArgTestBinary},
		}
		assert.Equal(t, expected, got)
	})
}

func TestClassifyArgs_Test_Aliases(t *testing.T) {
	t.Run("supported -test.run and --test.run", func(t *testing.T) {
		args := []string{"-test.run", "TestA", "--test.run=TestB", "-test.v", "./pkg"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-test.run", Kind: ArgTestFlag, FlagName: "-test.run", HasValue: false},
			{Index: 1, Raw: "TestA", Kind: ArgTestFlagValue, FlagName: "-test.run", Value: "TestA"},
			{
				Index:    2,
				Raw:      "--test.run=TestB",
				Kind:     ArgTestFlag,
				FlagName: "-test.run",
				HasValue: true,
				Value:    "TestB",
			},
			{Index: 3, Raw: "-test.v", Kind: ArgTestFlag, FlagName: "-test.v", HasValue: false},
			{Index: 4, Raw: "./pkg", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("unsupported -test.exec and -test.vet treated as unknown flags", func(t *testing.T) {
		args := []string{"-test.exec", "echo", "./pkg"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-test.exec", Kind: ArgTestBinary, FlagName: "-test.exec", HasValue: false},
			{Index: 1, Raw: "echo", Kind: ArgTestBinary, FlagName: "-test.exec", Value: "echo"},
			{Index: 2, Raw: "./pkg", Kind: ArgTestBinary},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("supported plain -exec and -vet", func(t *testing.T) {
		args := []string{"-exec", "echo", "-vet=off", "./pkg"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-exec", Kind: ArgTestFlag, FlagName: "-exec", HasValue: false},
			{Index: 1, Raw: "echo", Kind: ArgTestFlagValue, FlagName: "-exec", Value: "echo"},
			{Index: 2, Raw: "-vet=off", Kind: ArgTestFlag, FlagName: "-vet", HasValue: true, Value: "off"},
			{Index: 3, Raw: "./pkg", Kind: ArgTarget},
		}
		assert.Equal(t, expected, got)
	})
}

func TestClassifyArgs_Test_PackageListAndTestArgvTransitions(t *testing.T) {
	t.Run(
		"known flag ends the package list",
		func(t *testing.T) {
			args := []string{"./pkg", "-run", "TestX", "./other"}
			got := classifyArgs(subcmdTest, args)

			expected := []ClassifiedArg{
				{Index: 0, Raw: "./pkg", Kind: ArgTarget},
				{Index: 1, Raw: "-run", Kind: ArgTestFlag, FlagName: "-run", HasValue: false},
				{Index: 2, Raw: "TestX", Kind: ArgTestFlagValue, FlagName: "-run", Value: "TestX"},
				{Index: 3, Raw: "./other", Kind: ArgTestBinary},
			}
			assert.Equal(t, expected, got)
		},
	)

	t.Run(
		"positional after -run belongs to the test binary",
		func(t *testing.T) {
			args := []string{"fmt", "-run", "TestX", "math"}
			got := classifyArgs(subcmdTest, args)

			expected := []ClassifiedArg{
				{Index: 0, Raw: "fmt", Kind: ArgTarget},
				{Index: 1, Raw: "-run", Kind: ArgTestFlag, FlagName: "-run", HasValue: false},
				{Index: 2, Raw: "TestX", Kind: ArgTestFlagValue, FlagName: "-run", Value: "TestX"},
				{Index: 3, Raw: "math", Kind: ArgTestBinary},
			}
			assert.Equal(t, expected, got)
		},
	)

	t.Run(
		"flags before packages and a trailing test binary argument",
		func(t *testing.T) {
			args := []string{"-v", "fmt", "-run", "TestX", "math", "-tags=integration", "./other"}
			got := classifyArgs(subcmdTest, args)

			expected := []ClassifiedArg{
				{Index: 0, Raw: "-v", Kind: ArgBuildFlag, FlagName: "-v", HasValue: false},
				{Index: 1, Raw: "fmt", Kind: ArgTarget},
				{Index: 2, Raw: "-run", Kind: ArgTestFlag, FlagName: "-run", HasValue: false},
				{Index: 3, Raw: "TestX", Kind: ArgTestFlagValue, FlagName: "-run", Value: "TestX"},
				{Index: 4, Raw: "math", Kind: ArgTestBinary},
				{Index: 5, Raw: "-tags=integration", Kind: ArgTestBinary},
				{Index: 6, Raw: "./other", Kind: ArgTestBinary},
			}
			assert.Equal(t, expected, got)
		},
	)

	t.Run("positional tail preserves a build-like flag", func(t *testing.T) {
		args := []string{"./pkg", "-run", "TestX", "positional", "-tags=integration"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "-run", Kind: ArgTestFlag, FlagName: "-run", HasValue: false},
			{Index: 2, Raw: "TestX", Kind: ArgTestFlagValue, FlagName: "-run", Value: "TestX"},
			{Index: 3, Raw: "positional", Kind: ArgTestBinary},
			{Index: 4, Raw: "-tags=integration", Kind: ArgTestBinary},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("vendoring isolation after unjoined unknown flag and positional test arg", func(t *testing.T) {
		args := []string{"./pkg", "-custom", "value", "positional", "-mod=vendor"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "-custom", Kind: ArgTestBinary, FlagName: "-custom", HasValue: false},
			{Index: 2, Raw: "value", Kind: ArgTestBinary, FlagName: "-custom", Value: "value"},
			{Index: 3, Raw: "positional", Kind: ArgTestBinary},
			{Index: 4, Raw: "-mod=vendor", Kind: ArgTestBinary},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("joined unknown flag followed by positional", func(t *testing.T) {
		args := []string{"./pkg", "-custom=x", "positional", "-mod=vendor"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "-custom=x", Kind: ArgTestBinary, FlagName: "-custom", HasValue: true, Value: "x"},
			{Index: 2, Raw: "positional", Kind: ArgTestBinary},
			{Index: 3, Raw: "-mod=vendor", Kind: ArgTestBinary},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("unknown separated flag value optimistic behavior without package target", func(t *testing.T) {
		args := []string{"-custom", "value", "-run", "TestX"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "-custom", Kind: ArgTestBinary, FlagName: "-custom", HasValue: false},
			{Index: 1, Raw: "value", Kind: ArgTestBinary, FlagName: "-custom", Value: "value"},
			{Index: 2, Raw: "-run", Kind: ArgTestFlag, FlagName: "-run", HasValue: false},
			{Index: 3, Raw: "TestX", Kind: ArgTestFlagValue, FlagName: "-run", Value: "TestX"},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("unknown separated flag value optimistic behavior with package target", func(t *testing.T) {
		args := []string{"./pkg", "-custom", "value", "-run", "TestX"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "-custom", Kind: ArgTestBinary, FlagName: "-custom", HasValue: false},
			{Index: 2, Raw: "value", Kind: ArgTestBinary, FlagName: "-custom", Value: "value"},
			{Index: 3, Raw: "-run", Kind: ArgTestFlag, FlagName: "-run", HasValue: false},
			{Index: 4, Raw: "TestX", Kind: ArgTestFlagValue, FlagName: "-run", Value: "TestX"},
		}
		assert.Equal(t, expected, got)
	})

	t.Run("definitive positional tail classifies all subsequent tokens as test binary", func(t *testing.T) {
		args := []string{"./pkg", "-custom=x", "positional", "-race", "-mod=vendor", "-tags=x", "./other"}
		got := classifyArgs(subcmdTest, args)

		expected := []ClassifiedArg{
			{Index: 0, Raw: "./pkg", Kind: ArgTarget},
			{Index: 1, Raw: "-custom=x", Kind: ArgTestBinary, FlagName: "-custom", HasValue: true, Value: "x"},
			{Index: 2, Raw: "positional", Kind: ArgTestBinary},
			{Index: 3, Raw: "-race", Kind: ArgTestBinary},
			{Index: 4, Raw: "-mod=vendor", Kind: ArgTestBinary},
			{Index: 5, Raw: "-tags=x", Kind: ArgTestBinary},
			{Index: 6, Raw: "./other", Kind: ArgTestBinary},
		}
		assert.Equal(t, expected, got)
	})
}

func TestClassifyArgs_GoTestParity(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\ngo 1.26\n"), 0o644))
	for _, pkg := range []string{".", "a", "b"} {
		pkgDir := filepath.Join(dir, pkg)
		require.NoError(t, os.MkdirAll(pkgDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "probe_test.go"), []byte(`package probe
import (
 "encoding/json"
 "flag"
 "testing"
)
func TestProbe(t *testing.T) {
 args, err := json.Marshal(flag.Args())
 if err != nil { t.Fatal(err) }
 t.Logf("PROBE_ARGS=%s", args)
}
`), 0o644))
	}
	for _, args := range [][]string{
		{"./a", "./b", "-run", "TestProbe"},
		{"-run", "TestProbe", "./a", "./b"},
		{"./a", "-run", "TestProbe", "./b", "-tags=unused"},
		{"./a", "-v", "./b"},
		{"./a", "-args", "./b", "-tags=unused"},
		{"-run", "TestProbe"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), "go", append([]string{"test", "-json"}, args...)...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			var tested []string
			tail := []string{}
			for _, arg := range classifyArgs(subcmdTest, args) {
				if arg.Kind == ArgTestBinary {
					tail = append(tail, arg.Raw)
				}
			}
			tailJSON, err := json.Marshal(tail)
			require.NoError(t, err)
			loggedArgs := false
			for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
				var event struct {
					Action  string `json:"Action"`
					Package string `json:"Package"`
					Test    string `json:"Test"`
					Output  string `json:"Output"`
				}
				require.NoError(t, json.Unmarshal([]byte(line), &event))
				if event.Action == "run" && event.Test == "TestProbe" {
					pkg := "."
					if event.Package != "probe" {
						pkg = "./" + strings.TrimPrefix(event.Package, "probe/")
					}
					tested = append(tested, pkg)
				}
				if strings.Contains(event.Output, "PROBE_ARGS=") {
					loggedArgs = true
					assert.Contains(t, event.Output, "PROBE_ARGS="+string(tailJSON))
				}
			}
			assert.True(t, loggedArgs, "the probe must report its test binary arguments")
			pkgs, files, err := splitBuildTargets(subcmdTest, args)
			require.NoError(t, err)
			assert.Empty(t, files)
			if len(pkgs) == 0 {
				pkgs = []string{"."}
			}
			assert.ElementsMatch(t, pkgs, tested)
		})
	}
}
