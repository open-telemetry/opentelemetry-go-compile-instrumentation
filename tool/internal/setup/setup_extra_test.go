// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestCreateRuleFromFields_CallRule(t *testing.T) {
	raw := []byte("target: example.com/x\nfunction_call: net/http.Get\nreplace: tracedGet({{ . }})\n")
	fields := map[string]any{
		"target":             "example.com/x",
		rule.SelFunctionCall: "net/http.Get",
		"replace":            "tracedGet({{ . }})",
	}

	r, err := createRuleFromFields(raw, "call-rule", fields)
	require.NoError(t, err)
	require.IsType(t, &rule.InstCallRule{}, r)
}

func TestCreateRuleFromFields_UnrecognizedSelector(t *testing.T) {
	fields := map[string]any{"target": "example.com/x"}
	_, err := createRuleFromFields(nil, "bad-rule", fields)
	require.Error(t, err)
	require.ErrorContains(t, err, "no recognised selector")
}

func TestParseRuleFromYaml_NormalizeError(t *testing.T) {
	content := []byte(`bad:
  target: main
  where:
    target: net/http
    func: Open
  do:
    - inject_hooks:
        before: BeforeOpen
        path: example.com/hooks
`)
	_, err := parseRuleFromYaml(content)
	require.Error(t, err)
	require.ErrorContains(t, err, "target must be top-level")
}

func TestRunMatch_CgoFiles(t *testing.T) {
	dep := &Dependency{
		ImportPath: "example.com/cgo",
		CgoFiles:   map[string]string{"a.go": "header"},
	}

	sp := newTestSetupPhase()
	set, err := sp.runMatch(context.Background(), dep, nil, nil)
	require.NoError(t, err)
	require.True(t, set.IsEmpty())
}

func TestRunMatch_VersionFilteredOut(t *testing.T) {
	srcFile := writeGoSource(t, "v.go", "package v\n\nfunc Handler() {}\n")
	dep := &Dependency{
		ImportPath: "example.com/v",
		Version:    "v1.0.0",
		Sources:    []string{srcFile},
		CgoFiles:   map[string]string{},
	}
	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "vrule",
			Target:  "example.com/v",
			Version: "v2.0.0",
		},
		Func:   "Handler",
		Before: "BeforeHandler",
		Path:   "example.com/hooks",
	}

	sp := newTestSetupPhase()
	set, err := sp.runMatch(context.Background(), dep, map[string][]rule.InstRule{"example.com/v": {funcRule}}, nil)
	require.NoError(t, err)
	require.True(t, set.IsEmpty())
}

func TestPreciseMatching_NoSources(t *testing.T) {
	dep := &Dependency{ImportPath: "example.com/empty"}
	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "r", Target: "example.com/empty"},
		Func:         "Foo",
	}

	sp := newTestSetupPhase()
	set := rule.NewInstRuleSet(dep.ImportPath)
	res, err := sp.preciseMatching(context.Background(), dep, []rule.InstRule{funcRule}, set)
	require.NoError(t, err)
	require.True(t, res.IsEmpty())
}

func TestPreciseMatching_CtxCancelled(t *testing.T) {
	srcFile := writeGoSource(t, "c.go", "package c\n\nfunc Foo() {}\n")
	dep := &Dependency{
		ImportPath: "example.com/c",
		Sources:    []string{srcFile},
	}
	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "r", Target: "example.com/c"},
		Func:         "Foo",
		Before:       "BeforeFoo",
		Path:         "example.com/hooks",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sp := newTestSetupPhase()
	set := rule.NewInstRuleSet(dep.ImportPath)
	_, err := sp.preciseMatching(ctx, dep, []rule.InstRule{funcRule}, set)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPreciseMatching_MatchOneRuleError(t *testing.T) {
	srcFile := writeGoSource(t, "s.go", "package s\n\nfunc Foo(a string) error { return nil }\n")
	dep := &Dependency{
		ImportPath: "example.com/s",
		Sources:    []string{srcFile},
	}
	badRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "bad", Target: "example.com/s"},
		Func:         "Foo",
		Signature:    &rule.FuncSignature{Args: []string{"[]invalid"}},
	}

	sp := newTestSetupPhase()
	set := rule.NewInstRuleSet(dep.ImportPath)
	_, err := sp.preciseMatching(context.Background(), dep, []rule.InstRule{badRule}, set)
	require.Error(t, err)
}

func TestRulesFromDirWalkError(t *testing.T) {
	_, err := rulesFromDir(filepath.Join(t.TempDir(), "missing"), false)
	require.Error(t, err)
}

func TestLoadCustomRulesStatError(t *testing.T) {
	t.Setenv(util.EnvOtelcRules, "")
	_, err := loadCustomRules(filepath.Join(t.TempDir(), "nope.yaml"))
	require.Error(t, err)
}

func TestMatchDeps_NoRules(t *testing.T) {
	t.Setenv(util.EnvOtelcRules, "")

	sp := newTestSetupPhase()
	matched, err := sp.matchDeps(context.Background(), []*Dependency{{ImportPath: "example.com/x"}}, nil)
	require.NoError(t, err)
	require.Nil(t, matched)
}

func TestMatchDeps_RunMatchError(t *testing.T) {
	ruleFile := filepath.Join(t.TempDir(), "r.yaml")
	err := os.WriteFile(ruleFile, []byte(`h:
  target: example.com/bad
  func: Handler
  before: BeforeHandler
  path: "example.com/hooks"
`), 0o644)
	require.NoError(t, err)

	badSrc := writeGoSource(t, "bad.go", "not valid go {{{")
	sp := newTestSetupPhase()
	sp.ruleConfig = ruleFile

	_, err = sp.matchDeps(context.Background(), []*Dependency{
		{ImportPath: "example.com/bad", Sources: []string{badSrc}, CgoFiles: map[string]string{}},
	}, nil)
	require.Error(t, err)
}

func TestFindToolFilesBothExist(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, ToolFileCanonical), []byte("package main"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, ToolFileAlias), []byte("package main"), 0o644)
	require.NoError(t, err)

	_, err = findToolFiles(map[string]bool{dir: true})
	require.Error(t, err)
	require.ErrorContains(t, err, "only one instrumentation config file")
}

func TestLoadRules_FindToolFilesError(t *testing.T) {
	t.Setenv(util.EnvOtelcRules, "")
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, ToolFileCanonical), []byte("package main"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, ToolFileAlias), []byte("package main"), 0o644)
	require.NoError(t, err)

	sp := newTestSetupPhase()
	_, err = sp.loadRules(context.Background(), map[string]bool{dir: true})
	require.Error(t, err)
}

func TestCollectImports_UnquoteError(t *testing.T) {
	f := &dst.File{Imports: []*dst.ImportSpec{
		{Name: &dst.Ident{Name: "_"}, Path: &dst.BasicLit{Value: `"badpath`}},
	}}

	_, err := collectImports("tool.go", f, map[string]bool{})
	require.Error(t, err)
}

func TestWalkInstrumentationParseError(t *testing.T) {
	err := walkInstrumentation(
		context.Background(),
		[]string{filepath.Join(t.TempDir(), "nope.go")},
		func(v *InstrumentationVisit) (bool, error) { return false, nil },
	)
	require.Error(t, err)
}
