// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
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

	dest := filepath.Join(ip.debugArtifactDir(), "source.go.diff")
	assert.Contains(t, dest, "github_com_redis_go-redis_v9")
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

	ip := newTestPhase()
	ip.writeDiffForDebug(files.oldFile, files.newFile, changes)

	dest := filepath.Join(ip.debugArtifactDir(), "source.go.diff")
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

			ip := newTestPhase()
			ip.writeDiffForDebug(files.oldFile, files.newFile, nil)

			_, err := os.Stat(filepath.Join(ip.debugArtifactDir(), "source.go.diff"))
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

func TestWriteDiffForDebugWriteFailure(t *testing.T) {
	t.Setenv(util.EnvOtelcDebug, "1")

	// MkdirAll failure: work dir has a regular file at ".otelc-build"
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, util.BuildTempDir), []byte("not a dir"), 0o600))

	files := sourcePair(t, instrumentedSource)
	newTestPhase().writeDiffForDebug(files.oldFile, files.newFile, nil)

	// WriteFile failure: destination is an existing directory
	workDir2 := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir2)
	dest := filepath.Join(workDir2, util.BuildTempDir, "debug", filepath.Base(files.oldFile)+".diff")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	newTestPhase().writeDiffForDebug(files.oldFile, files.newFile, nil)
}

func TestWriteDiffForDebugEmptyRuleDiffSkipped(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "1")

	files := sourcePair(t, instrumentedSource)
	changes := []ruleChange{
		{name: "empty_diff", before: []byte("same\n"), after: []byte("same\n")},
	}
	ip := newTestPhase()
	ip.writeDiffForDebug(files.oldFile, files.newFile, changes)

	dest := filepath.Join(ip.debugArtifactDir(), "source.go.diff")
	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "=== rule 1/1: empty_diff ===")
}

func TestWriteDiffForDebugUnchangedOverallWithRuleChanges(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "1")

	files := sourcePair(t, originalSource) // both are originalSource
	changes := []ruleChange{
		{name: "temporary-edit", before: []byte("a\n"), after: []byte("b\n")},
	}
	ip := newTestPhase()
	ip.writeDiffForDebug(files.oldFile, files.newFile, changes)

	dest := filepath.Join(ip.debugArtifactDir(), "source.go.diff")
	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(content), "=== rule 1/1: temporary-edit ===")
}

func TestApplyRulesCapturingDiffsInitialRenderError(t *testing.T) {
	t.Setenv(util.EnvOtelcDebug, "1")
	render := func(*dst.File) ([]byte, error) {
		return nil, errors.New("simulated initial render failure")
	}

	hasFuncRule, changes, err := newTestPhase().applyRulesCapturingDiffsWithRenderer(
		context.Background(), nil, wrapVarDeclFile(), render)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rendering AST before applying rules")
	assert.False(t, hasFuncRule)
	assert.Nil(t, changes)
}

func TestApplyRulesCapturingDiffsPostRuleRenderError(t *testing.T) {
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

	callCount := 0
	render := func(f *dst.File) ([]byte, error) {
		callCount++
		// 1: initial render, 2: post ruleX render, 3: post ruleY render (fails)
		if callCount <= 2 {
			return ast.RenderFile(f)
		}
		return nil, errors.New("simulated post-rule render failure")
	}

	hasFuncRule, changes, err := newTestPhase().applyRulesCapturingDiffsWithRenderer(
		context.Background(), []rule.InstRule{ruleX, ruleY}, wrapVarDeclFile(), render)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rendering AST after applying rule wrap_y")
	assert.False(t, hasFuncRule)
	require.Len(t, changes, 1)
	assert.Equal(t, "wrap_x", changes[0].name)
}

func TestApplyRulesCapturingDiffsRenderErrorSkippedWhenDebugOff(t *testing.T) {
	t.Setenv(util.EnvOtelcDebug, "")
	render := func(*dst.File) ([]byte, error) {
		t.Fatal("render should not be called when debug is disabled")
		return nil, errors.New("should not be called")
	}

	ruleX := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "wrap_x"},
		Kind:         "var",
		Identifier:   "X",
		Wrap:         "double({{ . }})",
	}

	hasFuncRule, changes, err := newTestPhase().applyRulesCapturingDiffsWithRenderer(
		context.Background(), []rule.InstRule{ruleX}, wrapVarDeclFile(), render)
	require.NoError(t, err)
	assert.False(t, hasFuncRule)
	assert.Nil(t, changes)
}

func wrapFuncFile() *dst.File {
	return &dst.File{
		Name: &dst.Ident{Name: "main"},
		Decls: []dst.Decl{
			&dst.GenDecl{
				Tok: token.VAR,
				Specs: []dst.Spec{&dst.ValueSpec{
					Names:  []*dst.Ident{{Name: "X"}},
					Type:   &dst.Ident{Name: "int"},
					Values: []dst.Expr{&dst.BasicLit{Kind: token.INT, Value: "1"}},
				}},
			},
			&dst.FuncDecl{
				Name: &dst.Ident{Name: "main"},
				Type: &dst.FuncType{Params: &dst.FieldList{}},
				Body: &dst.BlockStmt{},
			},
		},
	}
}

func TestWriteFileRuleDiffForDebug(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	filePath := filepath.Join(workDir, "otelc.helper.go")
	content := "package main\n\nfunc Helper() string { return \"ok\" }\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	ip := newTestPhase()
	ip.compileArgs = []string{"-p", "example.com/mypkg"}
	ip.writeFileRuleDiffForDebug(filePath, "inject_helper")

	dest := filepath.Join(ip.debugArtifactDir(), "otelc.helper.go.diff")
	diffBytes, err := os.ReadFile(dest)
	require.NoError(t, err)
	diffText := string(diffBytes)

	assert.Contains(t, diffText, "=== rule: inject_helper ===")
	assert.Contains(t, diffText, "--- /dev/null")
	assert.Contains(t, diffText, "+++ "+filePath)
	assert.Contains(t, diffText, "+package main")
	assert.Contains(t, diffText, "+func Helper() string { return \"ok\" }")
}

func TestWriteGlobalsDiffForDebug(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	filePath := filepath.Join(workDir, "otelc.globals.go")
	content := "package main\n\nvar GlobalVal = 42\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	ip := newTestPhase()
	ip.compileArgs = []string{"-p", "example.com/mypkg"}
	ip.writeGlobalsDiffForDebug(filePath, []string{"rule_one", "rule_two"})

	dest := filepath.Join(ip.debugArtifactDir(), "otelc.globals.go.diff")
	diffBytes, err := os.ReadFile(dest)
	require.NoError(t, err)
	diffText := string(diffBytes)

	assert.Contains(t, diffText, "=== generated instrumentation file: otelc.globals.go ===")
	assert.Contains(t, diffText, "rules:\n  - rule_one\n  - rule_two")
	assert.Contains(t, diffText, "--- /dev/null")
	assert.Contains(t, diffText, "+++ "+filePath)
	assert.Contains(t, diffText, "+var GlobalVal = 42")
}

func TestWriteGlobalsDiffForDebug_NoRules(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	filePath := filepath.Join(workDir, "otelc.globals.go")
	content := "package main\n\nvar GlobalVal = 42\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	ip := newTestPhase()
	ip.writeGlobalsDiffForDebug(filePath, nil)

	dest := filepath.Join(ip.debugArtifactDir(), "otelc.globals.go.diff")
	diffBytes, err := os.ReadFile(dest)
	require.NoError(t, err)
	diffText := string(diffBytes)

	assert.Contains(t, diffText, "=== generated instrumentation file: otelc.globals.go ===")
	assert.NotContains(t, diffText, "rules:")
	assert.Contains(t, diffText, "--- /dev/null")
}

func TestWriteDiffForDebugRemovesStaleDiffOnNoOp(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	files := sourcePair(t, originalSource) // both are originalSource
	ip := newTestPhase()
	dest := filepath.Join(ip.debugArtifactDir(), "source.go.diff")
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, os.WriteFile(dest, []byte("stale diff content from build 1"), 0o644))

	ip.writeDiffForDebug(files.oldFile, files.newFile, nil)

	_, err := os.Stat(dest)
	assert.True(t, os.IsNotExist(err), "expected stale diff to be removed on no-op, stat returned %v", err)
}

func TestDebugArtifactDir_Layout(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	ip := newTestPhase()
	ip.compileArgs = []string{"-p", "example.com/pkg"}

	// Direct mode: uses sessions/<session>/<pkg>
	t.Setenv(util.EnvOtelcBuildSession, "")
	session := fmt.Sprintf("direct_%d", os.Getppid())
	expectedDirect := util.GetBuildTemp(filepath.Join("debug", "sessions", session, "example_com_pkg"))
	assert.Equal(t, expectedDirect, ip.debugArtifactDir())

	// Wrapper mode: uses debug/<pkg>
	t.Setenv(util.EnvOtelcBuildSession, "wrapper_12345")
	expectedWrapper := util.GetBuildTemp(filepath.Join("debug", "example_com_pkg"))
	assert.Equal(t, expectedWrapper, ip.debugArtifactDir())
}

func TestWriteDiffForDebugRemovesStaleDiffOnFileReadError(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	files := sourcePair(t, instrumentedSource)
	ip := newTestPhase()
	dest := filepath.Join(ip.debugArtifactDir(), filepath.Base(files.oldFile)+".diff")
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))

	// Pre-create stale diff
	require.NoError(t, os.WriteFile(dest, []byte("stale diff"), 0o644))
	// Missing oldFile with matching basename
	missingOld := filepath.Join(t.TempDir(), filepath.Base(files.oldFile))
	ip.writeDiffForDebug(missingOld, files.newFile, nil)
	_, err := os.Stat(dest)
	assert.True(t, os.IsNotExist(err), "expected stale diff to be removed when oldFile read fails")

	// Pre-create stale diff again
	require.NoError(t, os.WriteFile(dest, []byte("stale diff"), 0o644))
	// Missing newFile
	ip.writeDiffForDebug(files.oldFile, "/non/existent/new.go", nil)
	_, err = os.Stat(dest)
	assert.True(t, os.IsNotExist(err), "expected stale diff to be removed when newFile read fails")
}

func TestWriteAddedSourceDiffForDebugRemovesStaleDiffOnReadError(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	ip := newTestPhase()
	dest := filepath.Join(ip.debugArtifactDir(), "otelc.missing.go.diff")
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, os.WriteFile(dest, []byte("stale diff"), 0o644))

	ip.writeAddedSourceDiffForDebug("/non/existent/otelc.missing.go", "header")
	_, err := os.Stat(dest)
	assert.True(t, os.IsNotExist(err), "expected stale diff to be removed when added file read fails")
}

func TestWriteAddedSourceDiffForDebugSkipsWhenDebugOff(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	for _, debugVal := range []string{"", "0"} {
		t.Run("debug="+debugVal, func(t *testing.T) {
			t.Setenv(util.EnvOtelcDebug, debugVal)
			filePath := filepath.Join(workDir, "otelc.helper.go")
			require.NoError(t, os.WriteFile(filePath, []byte("package main\n"), 0o644))

			ip := newTestPhase()
			ip.writeFileRuleDiffForDebug(filePath, "my_rule")
			ip.writeGlobalsDiffForDebug(filePath, []string{"r1"})

			dest := util.GetBuildTemp(filepath.Join("debug", "otelc.helper.go.diff"))
			_, err := os.Stat(dest)
			assert.True(t, os.IsNotExist(err))
		})
	}
}

func TestCleanupDebugArtifacts(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	debugDir := util.GetBuildTemp("debug")
	pkgDir := filepath.Join(debugDir, "some_pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "file.go.diff"), []byte("diff content"), 0o644))

	require.DirExists(t, debugDir)
	require.NoError(t, CleanupDebugArtifacts())
	assert.NoDirExists(t, debugDir)
}

func TestApplyRulesCapturingDiffsTracksGlobalsContributors(t *testing.T) {
	t.Setenv(util.EnvOtelcDebug, "1")
	ruleX := &rule.InstRawRule{
		InstBaseRule: rule.InstBaseRule{Name: "raw_rule_x"},
		Func:         "main",
		Raw:          "println(1)",
	}
	ruleY := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "decl_rule_y"},
		Kind:         "var",
		Identifier:   "X",
		Wrap:         "double({{ . }})",
	}

	ip := newTestPhase()
	needsGlobals, _, err := ip.applyRulesCapturingDiffs(
		context.Background(), []rule.InstRule{ruleX, ruleY}, wrapFuncFile())
	require.NoError(t, err)
	assert.True(t, needsGlobals)
	assert.Equal(t, []string{"raw_rule_x"}, ip.globalsContributors)

	// Now check with debug off: contributors list is untouched
	t.Setenv(util.EnvOtelcDebug, "")
	ip2 := newTestPhase()
	needsGlobals2, _, err := ip2.applyRulesCapturingDiffs(
		context.Background(), []rule.InstRule{ruleX}, wrapFuncFile())
	require.NoError(t, err)
	assert.True(t, needsGlobals2)
	assert.Nil(t, ip2.globalsContributors)
}

func TestRemoveStaleDiff(t *testing.T) {
	t.Run("missing destination causes no warning or error", func(t *testing.T) {
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
		dest := filepath.Join(t.TempDir(), "nonexistent.diff")

		RemoveStaleDiff(dest, logger)
		assert.Empty(t, logs.String())
	})

	t.Run("successful stale diff removal", func(t *testing.T) {
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
		dest := filepath.Join(t.TempDir(), "stale.diff")
		require.NoError(t, os.WriteFile(dest, []byte("old diff"), 0o600))

		RemoveStaleDiff(dest, logger)
		assert.NoFileExists(t, dest)
		assert.Empty(t, logs.String())
	})

	t.Run("removal failure logs warning without build failure", func(t *testing.T) {
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
		dest := filepath.Join(t.TempDir(), "not-a-file.diff")
		// Create non-empty directory at dest so os.Remove fails portably.
		require.NoError(t, os.MkdirAll(filepath.Join(dest, "child"), 0o755))

		RemoveStaleDiff(dest, logger)
		assert.DirExists(t, dest)
		assert.Contains(t, logs.String(), "failed to remove stale instrumentation diff")
		assert.Contains(t, logs.String(), dest)
	})

	t.Run("instrumentPhase removeStaleDiff delegates to logger", func(t *testing.T) {
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
		ip := &instrumentPhase{logger: logger}
		dest := filepath.Join(t.TempDir(), "not-a-file.diff")
		require.NoError(t, os.MkdirAll(filepath.Join(dest, "child"), 0o755))

		ip.removeStaleDiff(dest)
		assert.DirExists(t, dest)
		assert.Contains(t, logs.String(), "failed to remove stale instrumentation diff")
		assert.Contains(t, logs.String(), dest)
	})
}

func TestDebugLifecycle_StalePackageCleaned(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	// Build 1: Package A compiles and produces a diff
	t.Setenv(util.EnvOtelcBuildSession, "direct_111111")
	require.NoError(t, EnsureDebugInitialized(t.Context()))

	pkgADir := DebugSessionDir("direct_111111", "example_com_pkg_a")
	require.NoError(t, os.MkdirAll(pkgADir, 0o755))
	diffA := filepath.Join(pkgADir, "a.go.diff")
	require.NoError(t, os.WriteFile(diffA, []byte("diff A"), 0o644))
	require.FileExists(t, diffA)

	// Build 2: Package A is cached (not compiled). Package B compiles.
	t.Setenv(util.EnvOtelcBuildSession, "direct_222222")
	require.NoError(t, EnsureDebugInitialized(t.Context()))

	pkgBDir := DebugSessionDir("direct_222222", "example_com_pkg_b")
	require.NoError(t, os.MkdirAll(pkgBDir, 0o755))
	diffB := filepath.Join(pkgBDir, "b.go.diff")
	require.NoError(t, os.WriteFile(diffB, []byte("diff B"), 0o644))

	// Stale diff A from Build 1 must be absent from Build 2's session
	diffAInBuild2 := filepath.Join(DebugSessionDir("direct_222222", "example_com_pkg_a"), "a.go.diff")
	assert.NoFileExists(t, diffAInBuild2)
	// Current diff B from Build 2 must exist
	assert.FileExists(t, diffB)
	// Build 1 (PID 111111 is dead) was pruned by CleanStaleSessions
	assert.NoFileExists(t, diffA)
	// Latest session points to Build 2
	latest, err := ReadDebugSession()
	require.NoError(t, err)
	assert.Equal(t, "direct_222222", latest)
}

func TestDebugLifecycle_CachedBuildCleaned(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	// Build 1: Package A compiles and produces a diff
	t.Setenv(util.EnvOtelcBuildSession, "direct_111111")
	require.NoError(t, EnsureDebugInitialized(t.Context()))

	pkgADir := DebugSessionDir("direct_111111", "example_com_pkg_a")
	require.NoError(t, os.MkdirAll(pkgADir, 0o755))
	diffA := filepath.Join(pkgADir, "a.go.diff")
	require.NoError(t, os.WriteFile(diffA, []byte("diff A"), 0o644))
	require.FileExists(t, diffA)

	// Build 2: Entire build served from cache (no compilation steps)
	t.Setenv(util.EnvOtelcBuildSession, "direct_222222")
	require.NoError(t, EnsureDebugInitialized(t.Context()))

	// Diff A must not masquerade as freshly generated by Build 2
	diffAInBuild2 := filepath.Join(DebugSessionDir("direct_222222", "example_com_pkg_a"), "a.go.diff")
	assert.NoFileExists(t, diffAInBuild2)
	// Dead Build 1 was cleaned
	assert.NoFileExists(t, diffA)
	// Latest points to Build 2
	latest, err := ReadDebugSession()
	require.NoError(t, err)
	assert.Equal(t, "direct_222222", latest)
}

func TestDebugLifecycle_ConcurrentChildren(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")
	session := fmt.Sprintf("direct_%d", os.Getpid())
	t.Setenv(util.EnvOtelcBuildSession, session)

	const numChildren = 10
	var wg sync.WaitGroup
	errCh := make(chan error, numChildren)

	for i := range numChildren {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := EnsureDebugInitialized(context.Background()); err != nil {
				errCh <- err
				return
			}
			pkgDir := DebugSessionDir(session, fmt.Sprintf("pkg_%d", idx))
			if err := os.MkdirAll(pkgDir, 0o755); err != nil {
				errCh <- err
				return
			}
			diffPath := filepath.Join(pkgDir, fmt.Sprintf("file_%d.go.diff", idx))
			if err := os.WriteFile(diffPath, fmt.Appendf(nil, "diff %d", idx), 0o644); err != nil {
				errCh <- err
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	// Verify all children's artifacts survived without deleting each other
	for i := range numChildren {
		diffPath := DebugSessionDir(session, fmt.Sprintf("pkg_%d", i), fmt.Sprintf("file_%d.go.diff", i))
		assert.FileExists(t, diffPath)
	}
}

func TestDebugLifecycle_ConcurrentDifferentSessions(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	// Both sessions use live process IDs so CleanStaleSessions recognizes them as actively running
	sessionA := fmt.Sprintf("direct_%d_a", os.Getpid())
	sessionB := fmt.Sprintf("direct_%d_b", os.Getppid())

	// Step 1: Session A child initializes and writes A.diff
	t.Setenv(util.EnvOtelcBuildSession, sessionA)
	require.NoError(t, EnsureDebugInitialized(t.Context()))
	pkgADir := DebugSessionDir(sessionA, "pkg_a")
	require.NoError(t, os.MkdirAll(pkgADir, 0o755))
	diffA := filepath.Join(pkgADir, "a.go.diff")
	require.NoError(t, os.WriteFile(diffA, []byte("diff A"), 0o644))
	require.FileExists(t, diffA)

	// Step 2: Session B child initializes concurrently / overlapping and writes B.diff
	t.Setenv(util.EnvOtelcBuildSession, sessionB)
	require.NoError(t, EnsureDebugInitialized(t.Context()))
	pkgBDir := DebugSessionDir(sessionB, "pkg_b")
	require.NoError(t, os.MkdirAll(pkgBDir, 0o755))
	diffB := filepath.Join(pkgBDir, "b.go.diff")
	require.NoError(t, os.WriteFile(diffB, []byte("diff B"), 0o644))
	require.FileExists(t, diffB)

	// Step 3: Another child from Session A initializes/continues
	t.Setenv(util.EnvOtelcBuildSession, sessionA)
	require.NoError(t, EnsureDebugInitialized(t.Context()))

	// Assert: Neither build deleted the other's active artifacts
	assert.FileExists(t, diffA, "Build A's active artifact must survive Session B initialization")
	assert.FileExists(t, diffB, "Build B's active artifact must survive subsequent Session A child initialization")

	// Step 4: Simultaneous session initialization using goroutines to exercise the lock/session path
	const workersPerSession = 5
	var wg sync.WaitGroup
	errCh := make(chan error, workersPerSession*2)

	for i := range workersPerSession {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			pDir := DebugSessionDir(sessionA, fmt.Sprintf("goroutine_pkg_a_%d", idx))
			if err := os.MkdirAll(pDir, 0o755); err != nil {
				errCh <- err
				return
			}
			fPath := filepath.Join(pDir, "file.diff")
			if err := os.WriteFile(fPath, []byte("a"), 0o644); err != nil {
				errCh <- err
				return
			}
		}(i)
		go func(idx int) {
			defer wg.Done()
			pDir := DebugSessionDir(sessionB, fmt.Sprintf("goroutine_pkg_b_%d", idx))
			if err := os.MkdirAll(pDir, 0o755); err != nil {
				errCh <- err
				return
			}
			fPath := filepath.Join(pDir, "file.diff")
			if err := os.WriteFile(fPath, []byte("b"), 0o644); err != nil {
				errCh <- err
				return
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	for i := range workersPerSession {
		assert.FileExists(t, DebugSessionDir(sessionA, fmt.Sprintf("goroutine_pkg_a_%d", i), "file.diff"))
		assert.FileExists(t, DebugSessionDir(sessionB, fmt.Sprintf("goroutine_pkg_b_%d", i), "file.diff"))
	}
}

func TestDebugLifecycle_SetupRetention(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	// Setup runs and produces otelc.runtime.go and otelc.runtime.go.diff
	pkgDir := util.GetBuildTemp(filepath.Join("debug", "example_com_app"))
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	runtimeSource := filepath.Join(pkgDir, "otelc.runtime.go")
	runtimeDiff := filepath.Join(pkgDir, "otelc.runtime.go.diff")
	require.NoError(t, os.WriteFile(runtimeSource, []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(runtimeDiff, []byte("diff runtime"), 0o644))
	require.NoError(t, RecordDebugSession("setup_12345"))

	// Build 1 runs after setup
	t.Setenv(util.EnvOtelcBuildSession, "direct_111111")
	require.NoError(t, EnsureDebugInitialized(t.Context()))

	session1PkgDir := DebugSessionDir("direct_111111", "example_com_app")
	assert.FileExists(t, filepath.Join(session1PkgDir, "otelc.runtime.go"))
	assert.FileExists(t, filepath.Join(session1PkgDir, "otelc.runtime.go.diff"))

	// Package compilation adds compiler diff
	appDiff := filepath.Join(session1PkgDir, "app.go.diff")
	require.NoError(t, os.WriteFile(appDiff, []byte("diff app"), 0o644))

	// Both setup runtime artifacts and compiler diffs exist in Build 1
	assert.FileExists(t, filepath.Join(session1PkgDir, "otelc.runtime.go"))
	assert.FileExists(t, filepath.Join(session1PkgDir, "otelc.runtime.go.diff"))
	assert.FileExists(t, appDiff)

	// Build 2 runs (direct mode without re-running setup)
	t.Setenv(util.EnvOtelcBuildSession, "direct_222222")
	require.NoError(t, EnsureDebugInitialized(t.Context()))

	session2PkgDir := DebugSessionDir("direct_222222", "example_com_app")
	// Setup runtime artifacts are preserved in Build 2
	assert.FileExists(t, filepath.Join(session2PkgDir, "otelc.runtime.go"))
	assert.FileExists(t, filepath.Join(session2PkgDir, "otelc.runtime.go.diff"))
	// Stale compiler diff is absent from Build 2
	assert.NoFileExists(t, filepath.Join(session2PkgDir, "app.go.diff"))
	// Stable setup runtime artifacts remain intact
	assert.FileExists(t, runtimeSource)
	assert.FileExists(t, runtimeDiff)
}

func TestDebugLifecycle_InitializationFailure(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	session := "direct_999999"
	t.Setenv(util.EnvOtelcBuildSession, session)
	readyMarker := debugSessionReadyMarker(session)

	failingCleaner := func(path string) error {
		return errors.New("simulated cleanup failure")
	}

	// Attempt 1: initialization must fail
	err := EnsureDebugInitializedWithCleaner(t.Context(), session, failingCleaner)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated cleanup failure")

	// Verify session is NOT marked ready
	assert.NoFileExists(t, readyMarker, "ready marker must not be written when cleanup fails")

	// Verify session is NOT recorded as current/latest
	latest, readErr := ReadDebugSession()
	require.NoError(t, readErr)
	assert.NotEqual(t, session, latest, "failed session must not be published as current")

	// Attempt 2: retry should succeed now that cleanup succeeds (using os.RemoveAll)
	require.NoError(t, EnsureDebugInitializedWithCleaner(t.Context(), session, os.RemoveAll))
	assert.FileExists(t, readyMarker, "ready marker must be written after successful initialization")

	latest, readErr = ReadDebugSession()
	require.NoError(t, readErr)
	assert.Equal(t, session, latest, "session must be published once initialization succeeds")
}

func TestCleanStaleCompilerArtifacts_ErrorSurfaced(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	debugDir := util.GetBuildTemp("debug")
	pkgDir := filepath.Join(debugDir, "example_com_pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))

	// Create a non-empty directory named file.go.diff so os.Remove fails portably
	unremovable := filepath.Join(pkgDir, "file.go.diff")
	require.NoError(t, os.MkdirAll(filepath.Join(unremovable, "child"), 0o755))

	err := CleanStaleCompilerArtifacts()
	require.Error(t, err, "CleanStaleCompilerArtifacts must surface filesystem errors rather than swallow them")
}

func TestToolexec_InitializationFailure_FailsCommand(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	// Create a file at .otelc-build/debug so directory creation fails
	debugPath := filepath.Join(workDir, ".otelc-build", "debug")
	require.NoError(t, os.MkdirAll(filepath.Dir(debugPath), 0o755))
	require.NoError(t, os.WriteFile(debugPath, []byte("blocking-file"), 0o644))

	err := Toolexec(t.Context(), []string{"go", "-V=full"}, false)
	require.Error(t, err, "Toolexec must fail when debug initialization fails under debug mode")
	assert.Contains(t, err.Error(), "failed to initialize debug artifacts")
}

func TestDebugLifecycle_DebugOffNoOp(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "0")

	require.NoError(t, EnsureDebugInitialized(t.Context()))
	assert.NoFileExists(t, debugLatestSessionFilePath())
}

func TestToolexec_DebugInitialization(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	// Call Toolexec with tool version probe (should intercept and initialize debug)
	_ = Toolexec(t.Context(), []string{"go", "-V=full"}, false)
	session, err := ReadDebugSession()
	require.NoError(t, err)
	assert.NotEmpty(t, session)

	// Call Toolexec nested, should skip debug initialization
	t.Setenv(util.EnvOtelcBuildSession, "new_nested_session")
	_ = Toolexec(t.Context(), []string{"go", "-V=full"}, true)
	sessionAfter, _ := ReadDebugSession()
	assert.Equal(t, session, sessionAfter)
}

func TestGetCurrentBuildSession(t *testing.T) {
	t.Setenv(util.EnvOtelcBuildSession, "custom_wrapper_session")
	assert.Equal(t, "custom_wrapper_session", GetCurrentBuildSession())

	t.Setenv(util.EnvOtelcBuildSession, "")
	assert.Equal(t, fmt.Sprintf("direct_%d", os.Getppid()), GetCurrentBuildSession())
}

func TestReadDebugSession_NotExist(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	session, err := ReadDebugSession()
	require.NoError(t, err)
	assert.Empty(t, session)
}

func TestCleanStaleCompilerArtifacts_NotExist(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	require.NoError(t, CleanStaleCompilerArtifacts())
}

func TestWriteAddedSourceDiff_MissingFile(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	dest := filepath.Join(workDir, "stale.diff")
	require.NoError(t, os.WriteFile(dest, []byte("stale diff content"), 0o644))

	WriteAddedSourceDiff(dest, filepath.Join(workDir, "nonexistent.go"), "header", nil)
	assert.NoFileExists(t, dest, "expected stale diff to be removed if added source cannot be read")
}

func TestWriteAddedSourceDiff_HeaderWithoutNewline(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	srcFile := filepath.Join(workDir, "added.go")
	require.NoError(t, os.WriteFile(srcFile, []byte("package test\n"), 0o644))

	dest := filepath.Join(workDir, "added.go.diff")
	WriteAddedSourceDiff(dest, srcFile, "# header line", nil)

	require.FileExists(t, dest)
	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(content), "# header line\n--- /dev/null"))
}
