//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

const lineDirectivesModule = "example.com/otelc-linedirectives"

// lineDirectivesMain is the app that otelc instruments. Each "// MARK:" comment
// gives the line of the statement that follows it. The test reads the lines
// from the source, so the source can change without changes to the assertions.
const lineDirectivesMain = `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"runtime"
)

// report prints the position of the call to report.
func report(label string) {
	_, file, line, _ := runtime.Caller(1)
	fmt.Printf("CALLER\t%s\t%s\t%d\n", label, file, line)
}

// Untouched has no rule. It is above the first injected code in this file.
func Untouched() {
	// MARK:untouched
	report("untouched")
}

// Target has a function rule and a raw rule.
func Target() {
	// MARK:original
	report("original")
}

// After has no rule. It is below the injected code in this file.
func After() {
	// MARK:after
	report("after")
}

func main() {
	Untouched()
	Target()
	After()
	UntouchedOther()
	RawOnly()
}
`

// lineDirectivesOther is a second file of the app. Only a raw rule changes it.
// A function rule adds an import at the top of the file, and this import is
// generated code. A raw rule does not add it, so the first declaration of this
// file is original code that is above the first injected code.
const lineDirectivesOther = `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// UntouchedOther has no rule. It is the first declaration in this file.
func UntouchedOther() {
	// MARK:untouched-other
	report("untouched-other")
}

// RawOnly has only a raw rule.
func RawOnly() {
	// MARK:raw-only
	report("raw-only")
}
`

const lineDirectivesHook = `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"
	"runtime"

	"go.opentelemetry.io/otelc/pkg/hook"
)

// Before prints the stack frames of the instrumented function.
func Before(ctx hook.HookContext) {
	_ = ctx
	pcs := make([]uintptr, 16)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(1, pcs)])
	for {
		frame, more := frames.Next()
		fmt.Printf("FRAME\t%s\t%s\t%d\n", frame.Function, frame.File, frame.Line)
		if !more {
			break
		}
	}
}
`

// lineDirectivesRules has a function rule, and a raw rule whose code calls
// runtime.Caller itself.
const lineDirectivesRules = `hook_target:
  target: main
  where:
    func: Target
  do:
    - inject_hooks:
        before: Before
        path: ` + lineDirectivesModule + `/hooks
raw_target:
  target: main
  where:
    func: Target
  do:
    - inject_code:
        raw: |-
          _, rawFile, rawLine, _ := runtime.Caller(0)
          fmt.Printf("RAW\t%s\t%d\n", rawFile, rawLine)
raw_only_target:
  target: main
  where:
    func: RawOnly
  do:
    - inject_code:
        raw: _ = 1
`

// stackLine is one line of the output of the app.
type stackLine struct {
	kind, label, function, file string
	line                        int
}

// TestLineDirectivesStackFrames builds an app through the real toolexec path,
// and checks the file and the line that runtime.Caller reports for injected
// code, and for original code.
func TestLineDirectivesStackFrames(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	mainMarks := markLines(t, lineDirectivesMain, 3)
	otherMarks := markLines(t, lineDirectivesOther, 2)

	for _, tc := range []struct {
		name  string
		flags []string
	}{
		{name: "default"},
		{name: "trimpath", flags: []string{"-trimpath"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trimpath := len(tc.flags) > 0
			moduleDir := t.TempDir()
			writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module %s

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, lineDirectivesModule, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
			writeTestFile(t, moduleDir, "main.go", lineDirectivesMain)
			writeTestFile(t, moduleDir, "other.go", lineDirectivesOther)
			writeTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), lineDirectivesHook)
			writeTestFile(t, moduleDir, "rules.yml", lineDirectivesRules)

			appName := "app"
			if filepath.Separator == '\\' {
				appName += ".exe"
			}
			appPath := filepath.Join(moduleDir, appName)
			env := append(os.Environ(), "OTELC_RULES=rules.yml")
			args := append([]string{"go", "build"}, tc.flags...)
			args = append(args, "-o", appPath, ".")
			runOtelcCommand(t, moduleDir, env, otelcPath, args...)
			output := runOtelcCommand(t, moduleDir, os.Environ(), appPath)

			lines := parseStackLines(t, output)

			wantMain := filepath.Join(moduleDir, "main.go")
			wantOther := filepath.Join(moduleDir, "other.go")
			wantGenerated := filepath.VolumeName(wantMain) + "/<generated>"
			if trimpath {
				wantMain = lineDirectivesModule + "/main.go"
				wantOther = lineDirectivesModule + "/other.go"
				wantGenerated = "/<generated>"
				if vol := filepath.VolumeName(moduleDir); vol != "" {
					wantGenerated = vol + wantGenerated
				}
			}

			// Hook frames: the trampoline is injected code in Target.
			var targetFrames int
			for _, l := range lines {
				if l.kind == "FRAME" && strings.HasSuffix(l.function, ".Target") {
					targetFrames++
					assertSameFile(t, wantGenerated, l.file, "hook frame in Target")
					assert.Positive(t, l.line)
				}
			}
			assert.Equal(t, 1, targetFrames, "output:\n%s", output)

			// Raw code reports its own position in the generated file.
			raws := filterLines(lines, "RAW")
			require.Len(t, raws, 1, "output:\n%s", output)
			assertSameFile(t, wantGenerated, raws[0].file, "raw code")
			assert.Positive(t, raws[0].line)

			// Original code, also above the first injected code, keeps its
			// original file and line. The compiler reads a copy of each
			// changed file in the work directory, so "untouched-other" is
			// in the wrong file if the pass does not restore the first
			// declaration of other.go.
			callers := map[string]stackLine{}
			for _, l := range filterLines(lines, "CALLER") {
				callers[l.label] = l
			}
			for _, want := range []struct {
				label string
				file  string
				marks map[string]int
			}{
				{label: "untouched", file: wantMain, marks: mainMarks},
				{label: "original", file: wantMain, marks: mainMarks},
				{label: "after", file: wantMain, marks: mainMarks},
				{label: "untouched-other", file: wantOther, marks: otherMarks},
				{label: "raw-only", file: wantOther, marks: otherMarks},
			} {
				l, ok := callers[want.label]
				require.True(t, ok, "missing %q in output:\n%s", want.label, output)
				assertSameFile(t, want.file, l.file, want.label)
				assert.Equal(t, want.marks[want.label]+1, l.line, "line of %s", want.label)
			}
		})
	}
}

// markLines returns the line of each "// MARK:" comment in src. src must have
// count marks.
func markLines(t *testing.T, src string, count int) map[string]int {
	t.Helper()
	marks := map[string]int{}
	for i, text := range strings.Split(src, "\n") {
		if _, label, ok := strings.Cut(strings.TrimSpace(text), "// MARK:"); ok {
			marks[label] = i + 1
		}
	}
	require.Len(t, marks, count)
	return marks
}

func parseStackLines(t *testing.T, output string) []stackLine {
	t.Helper()
	var lines []stackLine
	for text := range strings.SplitSeq(output, "\n") {
		fields := strings.Split(strings.TrimRight(text, "\r"), "\t")
		switch fields[0] {
		case "FRAME": // FRAME, function, file, line
			require.Len(t, fields, 4, text)
			n, err := strconv.Atoi(fields[3])
			require.NoError(t, err)
			lines = append(lines, stackLine{kind: "FRAME", function: fields[1], file: fields[2], line: n})
		case "CALLER": // CALLER, label, file, line
			require.Len(t, fields, 4, text)
			n, err := strconv.Atoi(fields[3])
			require.NoError(t, err)
			lines = append(lines, stackLine{kind: "CALLER", label: fields[1], file: fields[2], line: n})
		case "RAW": // RAW, file, line
			require.Len(t, fields, 3, text)
			n, err := strconv.Atoi(fields[2])
			require.NoError(t, err)
			lines = append(lines, stackLine{kind: "RAW", file: fields[1], line: n})
		}
	}
	return lines
}

func filterLines(lines []stackLine, kind string) []stackLine {
	var out []stackLine
	for _, l := range lines {
		if l.kind == kind {
			out = append(out, l)
		}
	}
	return out
}

// assertSameFile compares two file names. Both use "/" as separator, and the
// temporary directory can have symbolic links on some systems.
func assertSameFile(t *testing.T, want, got, what string) {
	t.Helper()
	if resolved, err := filepath.EvalSymlinks(filepath.FromSlash(got)); err == nil {
		got = resolved
	}
	if resolved, err := filepath.EvalSymlinks(filepath.FromSlash(want)); err == nil {
		want = resolved
	}
	assert.Equal(t, filepath.ToSlash(want), filepath.ToSlash(got), "file of %s", what)
}
