// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	originalSource     = "package main\n\nfunc main() {}\n"
	instrumentedSource = "package main\n\nfunc main() { println(\"hi\") }\n"
)

type sourceFiles struct {
	oldFile string
	newFile string
}

// sourcePair writes an original and an instrumented copy of the same file
// name into sibling directories, mirroring how the instrument phase leaves
// the original in place and writes its rewrite into the work directory.
func sourcePair(t *testing.T, instrumented string) sourceFiles {
	t.Helper()
	oldDir, newDir := t.TempDir(), t.TempDir()
	oldFile := filepath.Join(oldDir, "source.go")
	newFile := filepath.Join(newDir, "source.go")
	require.NoError(t, os.WriteFile(oldFile, []byte(originalSource), 0o644))
	require.NoError(t, os.WriteFile(newFile, []byte(instrumented), 0o644))
	return sourceFiles{oldFile: oldFile, newFile: newFile}
}

func TestWriteDiffForDebugWritesDiffUnderPackageDir(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "1")
	files := sourcePair(t, instrumentedSource)

	ip := newTestPhase()
	ip.compileArgs = []string{"-p", "github.com/redis/go-redis/v9"}
	ip.writeDiffForDebug(files.oldFile, files.newFile, nil)

	dest := util.GetBuildTemp(filepath.Join("debug", "github_com_redis_go-redis_v9", "source.go.diff"))
	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(content), "-func main() {}")
	assert.Contains(t, string(content), "+func main() { println(\"hi\") }")
	// No per-rule changes were passed, so the file is just the plain diff,
	// not a labeled section.
	assert.NotContains(t, string(content), "=== rule")
}

func TestWriteDiffForDebugIncludesPerRuleSections(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "1")
	files := sourcePair(t, instrumentedSource)
	changes := []ruleChange{
		{name: "add-hook-before", before: []byte("a\n"), after: []byte("a\nb\n")},
		{name: "add-hook-after", before: []byte("a\nb\n"), after: []byte("a\nb\nc\n")},
	}

	newTestPhase().writeDiffForDebug(files.oldFile, files.newFile, changes)

	dest := util.GetBuildTemp(filepath.Join("debug", "source.go.diff"))
	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	text := string(content)

	// Both rules appear, in application order, ahead of the full-diff summary.
	beforeIdx := strings.Index(text, "=== rule 1/2: add-hook-before ===")
	afterIdx := strings.Index(text, "=== rule 2/2: add-hook-after ===")
	fullIdx := strings.Index(text, "=== full diff:")
	require.NotEqual(t, -1, beforeIdx)
	require.NotEqual(t, -1, afterIdx)
	require.NotEqual(t, -1, fullIdx)
	assert.Less(t, beforeIdx, afterIdx, "rule 1 section should precede rule 2")
	assert.Less(t, afterIdx, fullIdx, "per-rule sections should precede the full-diff summary")
	assert.Contains(t, text, "+func main() { println(\"hi\") }")
}

func TestWriteDiffForDebugSkips(t *testing.T) {
	tests := []struct {
		name         string
		debug        string
		instrumented string
	}{
		{name: "debug unset", debug: "", instrumented: instrumentedSource},
		{name: "debug explicitly off", debug: "0", instrumented: instrumentedSource},
		{name: "content unchanged", debug: "1", instrumented: originalSource},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
			t.Setenv(util.EnvOtelcDebug, test.debug)
			files := sourcePair(t, test.instrumented)

			newTestPhase().writeDiffForDebug(files.oldFile, files.newFile, nil)

			_, err := os.Stat(util.GetBuildTemp(filepath.Join("debug", "source.go.diff")))
			assert.True(t, os.IsNotExist(err), "expected no diff file, stat returned %v", err)
		})
	}
}

// wrapVarDeclFile builds a two-declaration file (var X int = 1; var Y int =
// 2) so a test can apply one rule per variable and check each rule's
// contribution is captured independently.
func wrapVarDeclFile() *dst.File {
	decl := func(name, value string) dst.Decl {
		return &dst.GenDecl{
			Tok: token.VAR,
			Specs: []dst.Spec{&dst.ValueSpec{
				Names:  []*dst.Ident{{Name: name}},
				Type:   &dst.Ident{Name: "int"},
				Values: []dst.Expr{&dst.BasicLit{Kind: token.INT, Value: value}},
			}},
		}
	}
	return &dst.File{
		Name: &dst.Ident{Name: "main"},
		Decls: []dst.Decl{
			decl("X", "1"),
			decl("Y", "2"),
		},
	}
}

func TestApplyRulesCapturingDiffsAttributesEachRuleInOrder(t *testing.T) {
	t.Setenv(util.EnvOtelcDebug, "1")
	ruleX := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_x"},
		Kind:         "var",
		Identifier:   "X",
		Wrap:         "double({{ . }})",
	}
	ruleY := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_y"},
		Kind:         "var",
		Identifier:   "Y",
		Wrap:         "triple({{ . }})",
	}

	hasFuncRule, changes, err := newTestPhase().applyRulesCapturingDiffs(
		context.Background(), []rule.InstRule{ruleX, ruleY}, wrapVarDeclFile())

	require.NoError(t, err)
	assert.False(t, hasFuncRule, "decl rules never set hasFuncRule")
	require.Len(t, changes, 2)

	assert.Equal(t, "wrap_x", changes[0].name)
	assert.NotContains(t, string(changes[0].before), "double(1)")
	assert.Contains(t, string(changes[0].after), "double(1)")

	assert.Equal(t, "wrap_y", changes[1].name)
	assert.NotContains(t, string(changes[1].before), "triple(2)")
	assert.Contains(t, string(changes[1].after), "triple(2)")
}

func TestApplyRulesCapturingDiffsSkipsCaptureWhenDebugOff(t *testing.T) {
	t.Setenv(util.EnvOtelcDebug, "")
	ruleX := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_x"},
		Kind:         "var",
		Identifier:   "X",
		Wrap:         "double({{ . }})",
	}
	root := wrapVarDeclFile()

	_, changes, err := newTestPhase().applyRulesCapturingDiffs(
		context.Background(), []rule.InstRule{ruleX}, root)

	require.NoError(t, err)
	assert.Nil(t, changes, "no snapshots should be captured with debug off")

	// The rule itself must still have been applied: debug only gates the
	// diff capture, never the actual instrumentation.
	spec := root.Decls[0].(*dst.GenDecl).Specs[0].(*dst.ValueSpec)
	call, ok := spec.Values[0].(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr after wrap, got %T", spec.Values[0])
	fn, ok := call.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "double", fn.Name)
}

func TestWriteDiffForDebugMissingFiles(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "1")

	// Missing oldFile
	newTestPhase().writeDiffForDebug("/non/existent/old.go", "/non/existent/new.go", nil)

	// Missing newFile
	files := sourcePair(t, instrumentedSource)
	newTestPhase().writeDiffForDebug(files.oldFile, "/non/existent/new.go", nil)

	_, err := os.Stat(util.GetBuildTemp(filepath.Join("debug", "source.go.diff")))
	assert.True(t, os.IsNotExist(err), "expected no diff file, stat returned %v", err)
}

func TestApplyRulesCapturingDiffsRuleError(t *testing.T) {
	t.Setenv(util.EnvOtelcDebug, "1")
	failingRule := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "bad_rule"},
		Kind:         "var",
		Identifier:   "X",
		Wrap:         "{{ invalid template",
	}

	_, _, err := newTestPhase().applyRulesCapturingDiffs(
		context.Background(), []rule.InstRule{failingRule}, wrapVarDeclFile())
	require.Error(t, err)
}
