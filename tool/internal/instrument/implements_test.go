// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

const implementsTestPkgPath = "example.com/sample"

// frameworkSource stands in for a web framework whose request context
// implements context.Context, such as *gin.Context.
const frameworkSource = `package framework

import "time"

type Context struct{}

func (*Context) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*Context) Done() <-chan struct{}       { return nil }
func (*Context) Err() error                  { return nil }
func (*Context) Value(any) any               { return nil }
`

const implementsSource = `package sample

import (
	"context"
	"net/http"
	"time"

	"example.com/framework"
)

type ReqCtx struct{ context.Context }

type ValueCtx struct{}

func (ValueCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ValueCtx) Done() <-chan struct{}       { return nil }
func (ValueCtx) Err() error                  { return nil }
func (ValueCtx) Value(any) any               { return nil }

type PtrCtx struct{}

func (*PtrCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*PtrCtx) Done() <-chan struct{}       { return nil }
func (*PtrCtx) Err() error                  { return nil }
func (*PtrCtx) Value(any) any               { return nil }

type MyErr struct{}

func (*MyErr) Error() string { return "" }

type Box[T any] struct{}

func direct(a int, ctx context.Context)                   {}
func embeddedPtr(c *ReqCtx)                               {}
func embeddedValue(c ReqCtx)                              {}
func valueReceiver(v ValueCtx)                            {}
func pointerReceiverValue(v PtrCtx)                       {}
func pointerReceiverPtr(v *PtrCtx)                        {}
func otherPackage(r *http.Request, c *framework.Context)  {}
func unnamed(int, *ReqCtx)                                {}
func blank(_ int, _ *ReqCtx)                              {}
func noMatch(a int, r *http.Request)                      {}
func typeParam[T context.Context](a int, t T)             {}
func typeParamAny[T any](t T)                             {}
func (c *ReqCtx) method(ctx context.Context)              {}
func (c *ReqCtx) receiverOnly(n int)                      {}
func (b *Box[T]) genericMethod(v T, ctx context.Context)  {}
func unnamedResults() (int, *MyErr)                       { return 0, nil }
func namedResults() (n int, err *MyErr)                   { return 0, nil }
func valueResult() MyErr                                  { return MyErr{} }
func noResults()                                          {}
func onlyErr() *MyErr                                     { return nil }
func errFirst() (*MyErr, int)                             { return nil, 0 }
func iface() error                                        { return nil }
func both(c *ReqCtx) (n int, err *MyErr)                  { return 0, nil }
`

// goToolPath returns the path of the named tool in GOTOOLDIR.
func goToolPath(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOTOOLDIR").Output()
	require.NoError(t, err)
	return filepath.Join(strings.TrimSpace(string(out)), name)
}

// stdlibArchives returns the export data archives of pkgs, by import path.
func stdlibArchives(t *testing.T, pkgs ...string) map[string]string {
	t.Helper()
	args := append([]string{"list", "-export", "-f", "{{.ImportPath}}={{.Export}}"}, pkgs...)
	out, err := exec.CommandContext(context.Background(), "go", args...).Output()
	require.NoError(t, err)
	archives := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		path, archive, ok := strings.Cut(line, "=")
		require.True(t, ok, "unexpected go list output %q", line)
		archives[path] = archive
	}
	return archives
}

// compileTestPackage compiles source as package pkgPath and returns the
// path of its archive.
func compileTestPackage(t *testing.T, dir, pkgPath, source string, archives map[string]string) string {
	t.Helper()
	src := filepath.Join(dir, filepath.Base(pkgPath)+".go")
	require.NoError(t, os.WriteFile(src, []byte(source), 0o600))
	var cfg strings.Builder
	for path, archive := range archives {
		fmt.Fprintf(&cfg, "packagefile %s=%s\n", path, archive)
	}
	cfgPath := filepath.Join(dir, filepath.Base(pkgPath)+".importcfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg.String()), 0o600))
	archive := filepath.Join(dir, filepath.Base(pkgPath)+".a")
	cmd := exec.CommandContext(context.Background(), goToolPath(t, "compile"),
		"-p", pkgPath, "-importcfg", cfgPath, "-o", archive, src)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "compiling %s: %s", pkgPath, out)
	return archive
}

// setupImplementsPhase parses source as the only file of package
// example.com/sample, with context, net/http, time and example.com/framework
// available, except the packages in omit.
func setupImplementsPhase(t *testing.T, source string, omit ...string) (*instrumentPhase, *dst.File) {
	t.Helper()
	dir := t.TempDir()
	archives := stdlibArchives(t, "context", "net/http", "time")
	archives["example.com/framework"] = compileTestPackage(t, dir, "example.com/framework", frameworkSource, archives)
	for _, path := range omit {
		delete(archives, path)
	}

	path := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	ip := &instrumentPhase{
		logger:       slog.New(slog.DiscardHandler),
		compileArgs:  []string{"compile", "-p", implementsTestPkgPath, path},
		importConfig: imports.ImportConfig{PackageFile: archives},
	}
	root, err := ip.parseFile(path)
	require.NoError(t, err)
	return ip, root
}

func TestThatImplementsHelpers(t *testing.T) {
	ip, root := setupImplementsPhase(t, implementsSource)
	require.NotNil(t, ip.ensureMethodCallInfo())
	require.NoError(t, ip.methodCallInfo.firstTypeError)
	importAliases := ast.ImportAliasMap(root, nil)

	newData := func(name string) *funcTemplateData {
		data := newFuncTemplateData(findFuncDeclInFile(t, root, name), nil, importAliases, "h1")
		data.typeInfo = ip.fileTypes()
		return data
	}

	argCases := []struct {
		fn    string
		iface string
		want  string // "#N" means the name of argument N
	}{
		{"direct", "context.Context", "ctx"},
		{"embeddedPtr", "context.Context", "c"},
		{"embeddedValue", "context.Context", "c"},
		{"valueReceiver", "context.Context", "v"},
		{"pointerReceiverValue", "context.Context", ""},
		{"pointerReceiverPtr", "context.Context", "v"},
		{"otherPackage", "context.Context", "c"},
		{"unnamed", "context.Context", "#1"},
		{"blank", "context.Context", "#1"},
		{"noMatch", "context.Context", ""},
		{"typeParam", "context.Context", "t"},
		{"typeParamAny", "context.Context", ""},
		{"method", "context.Context", "ctx"},
		{"receiverOnly", "context.Context", ""},
		{"genericMethod", "context.Context", "ctx"},
		{"otherPackage", "example.com/missing.Iface", ""},
		{"typeParamAny", "any", "t"},
	}
	for _, tc := range argCases {
		t.Run("arg/"+tc.fn+"/"+tc.iface, func(t *testing.T) {
			data := newData(tc.fn)
			got, err := data.FuncArgumentThatImplements(tc.iface)
			require.NoError(t, err)
			want := tc.want
			if idx, isIndex := strings.CutPrefix(tc.want, "#"); isIndex {
				var n int
				_, scanErr := fmt.Sscan(idx, &n)
				require.NoError(t, scanErr)
				want, err = data.FuncArgument(n)
				require.NoError(t, err)
				assert.True(t, strings.HasPrefix(want, ignoredParam), "want a synthetic name, got %q", want)
			}
			assert.Equal(t, want, got)
		})
	}

	retCases := []struct {
		fn   string
		want string // "#N" means the name of return value N
	}{
		{"unnamedResults", "#1"},
		{"namedResults", "err"},
		{"valueResult", ""},
		{"noResults", ""},
		{"onlyErr", "#0"},
		{"iface", "#0"},
	}
	for _, tc := range retCases {
		t.Run("ret/"+tc.fn, func(t *testing.T) {
			data := newData(tc.fn)
			got, err := data.FuncReturnThatImplements("error")
			require.NoError(t, err)
			want := tc.want
			if idx, isIndex := strings.CutPrefix(tc.want, "#"); isIndex {
				var n int
				_, scanErr := fmt.Sscan(idx, &n)
				require.NoError(t, scanErr)
				want, err = data.FuncReturn(n)
				require.NoError(t, err)
				assert.True(t, strings.HasPrefix(want, unnamedRetValName), "want a synthetic name, got %q", want)
			}
			assert.Equal(t, want, got)
		})
	}

	for _, iface := range []string{"int", "*context.Context", "net/http.Request", "context.Background", "not a type"} {
		t.Run("error/"+iface, func(t *testing.T) {
			_, err := newData("direct").FuncArgumentThatImplements(iface)
			require.Error(t, err)
			_, err = newData("onlyErr").FuncReturnThatImplements(iface)
			require.Error(t, err)
		})
	}
}

func TestThatImplementsHelpers_WithoutTypeInfoMatchTypeName(t *testing.T) {
	ip, root := setupImplementsPhase(t, implementsSource)
	importAliases := ast.ImportAliasMap(root, nil)
	for _, typeInfo := range []*funcTypes{nil, ip.fileTypes()} { // ip has not type checked
		data := newFuncTemplateData(findFuncDeclInFile(t, root, "direct"), nil, importAliases, "h1")
		data.typeInfo = typeInfo
		got, err := data.FuncArgumentThatImplements("context.Context")
		require.NoError(t, err)
		assert.Equal(t, "ctx", got)

		data = newFuncTemplateData(findFuncDeclInFile(t, root, "embeddedPtr"), nil, importAliases, "h1")
		data.typeInfo = typeInfo
		got, err = data.FuncArgumentThatImplements("context.Context")
		require.NoError(t, err)
		assert.Empty(t, got)

		data = newFuncTemplateData(findFuncDeclInFile(t, root, "iface"), nil, importAliases, "h1")
		data.typeInfo = typeInfo
		got, err = data.FuncReturnThatImplements("error")
		require.NoError(t, err)
		assert.NotEmpty(t, got)

		_, err = data.FuncReturnThatImplements("int")
		require.Error(t, err, "a predeclared non-interface type is an error even without type information")
	}
}

func TestThatImplementsHelpers_TypeErrorFallsBackToTypeName(t *testing.T) {
	ip, root := setupImplementsPhase(t, `package sample

import "context"

type ReqCtx struct{ context.Context }

func undefinedFirst(x Undefined, c *ReqCtx) {}
func undefinedCtx(c Undefined, ctx context.Context) {}
func undefinedOnly(c Undefined) {}
`)
	require.NotNil(t, ip.ensureMethodCallInfo())
	require.Error(t, ip.methodCallInfo.firstTypeError)
	importAliases := ast.ImportAliasMap(root, nil)

	for fn, want := range map[string]string{
		"undefinedFirst": "c",
		"undefinedCtx":   "ctx",
		"undefinedOnly":  "",
	} {
		data := newFuncTemplateData(findFuncDeclInFile(t, root, fn), nil, importAliases, "h1")
		data.typeInfo = ip.fileTypes()
		got, err := data.FuncArgumentThatImplements("context.Context")
		require.NoError(t, err)
		assert.Equal(t, want, got, fn)
	}
}

func TestThatImplementsHelpers_InvalidParamTypeMatchesTypeName(t *testing.T) {
	// The import of context fails, so ctx has an invalid type, but net/http
	// still makes context.Context visible.
	ip, root := setupImplementsPhase(t, `package sample

import (
	"context"
	"net/http"
)

func handle(r *http.Request, ctx context.Context) {}
`, "context")
	require.NotNil(t, ip.ensureMethodCallInfo())
	require.Error(t, ip.methodCallInfo.firstTypeError)
	iface, err := ip.fileTypes().lookupInterface("context.Context")
	require.NoError(t, err)
	require.NotNil(t, iface, "net/http's export data must make context.Context visible")

	data := newFuncTemplateData(findFuncDeclInFile(t, root, "handle"), nil, ast.ImportAliasMap(root, nil), "h1")
	data.typeInfo = ip.fileTypes()
	got, err := data.FuncArgumentThatImplements("context.Context")
	require.NoError(t, err)
	assert.Equal(t, "ctx", got)
}

func TestThatImplementsHelpers_InterfacePackageNotVisible(t *testing.T) {
	// Neither this package nor the export data of framework refers to
	// package context, so context.Context cannot be resolved.
	ip, root := setupImplementsPhase(t, `package sample

import "example.com/framework"

func handle(c *framework.Context) {}
`)
	require.NotNil(t, ip.ensureMethodCallInfo())
	require.NoError(t, ip.methodCallInfo.firstTypeError)

	data := newFuncTemplateData(findFuncDeclInFile(t, root, "handle"), nil, ast.ImportAliasMap(root, nil), "h1")
	data.typeInfo = ip.fileTypes()
	got, err := data.FuncArgumentThatImplements("context.Context")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestCallTemplate_ThatImplementsHelpers(t *testing.T) {
	ip, root := setupImplementsPhase(t, implementsSource)
	require.NotNil(t, ip.ensureMethodCallInfo())
	importAliases := ast.ImportAliasMap(root, nil)

	tmpl, err := newCallTemplate(
		`wrap({{ .FuncArgumentThatImplements "context.Context" }}, {{ .FuncReturnThatImplements "error" }}, {{ . }})`)
	require.NoError(t, err)
	tmpl.typeInfo = ip.fileTypes()

	call := &dst.CallExpr{Fun: &dst.Ident{Name: "f"}}
	expr, err := tmpl.compileExpression(call, findFuncDeclInFile(t, root, "both"), importAliases, nil)
	require.NoError(t, err)
	got, err := ast.RenderExpr(expr)
	require.NoError(t, err)
	assert.Equal(t, `wrap(c, err, f())`, got)

	_, err = tmpl.compileExpression(call, nil, importAliases, nil)
	require.Error(t, err, "the helpers need an enclosing function")
}

func TestMatchesImplementsFilters(t *testing.T) {
	ip, root := setupImplementsPhase(t, implementsSource)
	require.NotNil(t, ip.ensureMethodCallInfo())

	cases := []struct {
		name string
		fn   string
		r    rule.InstFuncRule
		want bool
	}{
		{"param embedded", "embeddedPtr", rule.InstFuncRule{ParamImplements: "context.Context"}, true},
		{"param other package", "otherPackage", rule.InstFuncRule{ParamImplements: "context.Context"}, true},
		{"param none", "noMatch", rule.InstFuncRule{ParamImplements: "context.Context"}, false},
		{
			"param value with pointer methods", "pointerReceiverValue",
			rule.InstFuncRule{ParamImplements: "context.Context"},
			false,
		},
		{"receiver is not a param", "receiverOnly", rule.InstFuncRule{ParamImplements: "context.Context"}, false},
		{"last result concrete", "onlyErr", rule.InstFuncRule{LastResultImplements: "error"}, true},
		{"last result not last", "errFirst", rule.InstFuncRule{LastResultImplements: "error"}, false},
		{"result not last", "errFirst", rule.InstFuncRule{ResultImplements: "error"}, true},
		{"last result none", "noResults", rule.InstFuncRule{LastResultImplements: "error"}, false},
		{"result none", "noResults", rule.InstFuncRule{ResultImplements: "error"}, false},
		{"all", "unnamedResults", rule.InstFuncRule{ResultImplements: "error", LastResultImplements: "error"}, true},
		{
			"all with failing param", "unnamedResults",
			rule.InstFuncRule{ParamImplements: "context.Context", LastResultImplements: "error"},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ip.matchesImplementsFilters(findFuncDeclInFile(t, root, tc.fn), &tc.r, root)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	_, err := ip.matchesImplementsFilters(findFuncDeclInFile(t, root, "direct"),
		&rule.InstFuncRule{ParamImplements: "net/http.Request"}, root)
	require.Error(t, err)
}

func TestInstrument_TypeChecksOnlyForImplements(t *testing.T) {
	for _, tc := range []struct {
		template  string
		wantCheck bool
	}{
		{`println({{ .FuncArgumentOfType "context.Context" }})`, false},
		{`println({{ .FuncArgumentThatImplements "context.Context" }})`, true},
		{`println({{ .FuncReturnThatImplements "error" }} == nil)`, true},
	} {
		t.Run(tc.template, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "p.go")
			require.NoError(t, os.WriteFile(src, []byte(`package p

import "context"

//otelc:span
func F(ctx context.Context) error { return nil }
`), 0o600))
			r := &rule.InstDirectiveRule{
				InstBaseRule: rule.InstBaseRule{Name: "span"},
				Directive:    "otelc:span",
				Template:     tc.template,
			}
			rset := rule.NewInstRuleSet("example.com/p")
			rset.PackageName = "p"
			rset.AddDirectiveRule(src, r)

			ip := newTestPhase()
			ip.workDir = t.TempDir()
			ip.compileArgs = []string{"compile", "-p", "example.com/p", src}
			require.NoError(t, ip.instrument(context.Background(), rset))
			assert.Equal(t, tc.wantCheck, ip.methodCallInfoLoaded)
		})
	}
}

func TestRsetUsesImplements(t *testing.T) {
	newSet := func() *rule.InstRuleSet { return rule.NewInstRuleSet("example.com/p") }
	file := filepath.Join(t.TempDir(), "f.go")

	rset := newSet()
	rset.AddFuncRule(file, &rule.InstFuncRule{Func: "F", Param: "context.Context"})
	rset.AddRawRule(file, &rule.InstRawRule{Func: "F", Raw: `{{ .FuncArgumentOfType "error" }}`})
	rset.AddCallRule(file, &rule.InstCallRule{Replace: `w({{ . }})`})
	assert.False(t, rsetUsesImplements(rset))

	rset = newSet()
	rset.AddFuncRule(file, &rule.InstFuncRule{Func: "F", LastResultImplements: "error"})
	assert.True(t, rsetUsesImplements(rset))

	rset = newSet()
	rset.AddRawRule(file, &rule.InstRawRule{Func: "F", Raw: `{{ .FuncReturnThatImplements "error" }}`})
	assert.True(t, rsetUsesImplements(rset))

	rset = newSet()
	rset.AddCallRule(
		file,
		&rule.InstCallRule{Replace: `w({{ .FuncArgumentThatImplements "context.Context" }}, {{ . }})`},
	)
	assert.True(t, rsetUsesImplements(rset))
}
