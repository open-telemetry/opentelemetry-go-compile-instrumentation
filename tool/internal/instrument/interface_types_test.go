// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"errors"
	"go/token"
	"go/types"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func setupCrossPkgInterfaceCase(
	t *testing.T,
	aSrc, bSrc, targetSrc, hookSrc string,
) (*instrumentPhase, *dst.File, *rule.InstFuncRule) {
	t.Helper()
	dir := t.TempDir()
	archives := map[string]string{
		"example.com/a": compileTestPackage(t, dir, "example.com/a", aSrc, map[string]string{}),
		"example.com/b": compileTestPackage(t, dir, "example.com/b", bSrc, map[string]string{}),
	}
	targetPath := filepath.Join(dir, "lib.go")
	require.NoError(t, os.WriteFile(targetPath, []byte(targetSrc), 0o600))

	hooksDir := filepath.Join(dir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "hook.go"), []byte(hookSrc), 0o600))

	ip := &instrumentPhase{
		logger:       slog.New(slog.DiscardHandler),
		compileArgs:  []string{"compile", "-p", "example.com/lib", targetPath},
		importConfig: imports.ImportConfig{PackageFile: archives},
	}
	root, err := ip.parseFile(targetPath)
	require.NoError(t, err)

	r := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "embedded_iface"},
		Path:         "example.com/hooks",
		ResolvedPath: hooksDir,
	}
	return ip, root, r
}

func TestEmbeddedInterfaceHookSignatures(t *testing.T) {
	const (
		emptyPkgA  = "package a\n\ntype Marker interface{}\n"
		emptyPkgB  = "package b\n\ntype Marker interface{}\n"
		methodPkgA = "package a\n\ntype Marker interface{ Tag() string }\n"
		methodPkgB = "package b\n\ntype Marker interface{ Tag() string }\n"
		otherPkgB  = "package b\n\ntype Marker interface{ Other() string }\n"
	)

	targetSrc := `package lib

import "example.com/a"

func Use(x interface{ a.Marker }) {}
func Get() interface{ a.Marker } { return nil }
`

	t.Run("rejects empty embedded hook interface against non-empty target", func(t *testing.T) {
		hookSrc := `package hooks

import "example.com/b"

type HookContext interface{}

func BeforeUse(ctx HookContext, x interface{ b.Marker }) {}
func AfterGet(ctx HookContext, r interface{ b.Marker }) {}
`
		ip, root, baseRule := setupCrossPkgInterfaceCase(t, methodPkgA, emptyPkgB, targetSrc, hookSrc)

		beforeRule := *baseRule
		beforeRule.Func = "Use"
		beforeRule.Before = "BeforeUse"
		_, err := ip.applyFuncRule(t.Context(), &beforeRule, root, findFuncDeclInFile(t, root, "Use"), true)
		require.ErrorContains(t, err, "type mismatch")

		afterRule := *baseRule
		afterRule.Func = "Get"
		afterRule.After = "AfterGet"
		_, err = ip.applyFuncRule(t.Context(), &afterRule, root, findFuncDeclInFile(t, root, "Get"), true)
		require.ErrorContains(t, err, "type mismatch")
	})

	t.Run("rejects non-empty embedded hook interface against empty target", func(t *testing.T) {
		hookSrc := `package hooks

import "example.com/b"

type HookContext interface{}

func BeforeUse(ctx HookContext, x interface{ b.Marker }) {}
func AfterGet(ctx HookContext, r interface{ b.Marker }) {}
`
		ip, root, baseRule := setupCrossPkgInterfaceCase(t, emptyPkgA, methodPkgB, targetSrc, hookSrc)

		beforeRule := *baseRule
		beforeRule.Func = "Use"
		beforeRule.Before = "BeforeUse"
		_, err := ip.applyFuncRule(t.Context(), &beforeRule, root, findFuncDeclInFile(t, root, "Use"), true)
		require.ErrorContains(t, err, "type mismatch")

		afterRule := *baseRule
		afterRule.Func = "Get"
		afterRule.After = "AfterGet"
		_, err = ip.applyFuncRule(t.Context(), &afterRule, root, findFuncDeclInFile(t, root, "Get"), true)
		require.ErrorContains(t, err, "type mismatch")
	})

	t.Run("rejects mismatched non-empty embedded interface methods", func(t *testing.T) {
		hookSrc := `package hooks

import "example.com/b"

type HookContext interface{}

func BeforeUse(ctx HookContext, x interface{ b.Marker }) {}
`
		ip, root, baseRule := setupCrossPkgInterfaceCase(t, methodPkgA, otherPkgB, targetSrc, hookSrc)
		baseRule.Func = "Use"
		baseRule.Before = "BeforeUse"
		_, err := ip.applyFuncRule(t.Context(), baseRule, root, findFuncDeclInFile(t, root, "Use"), true)
		require.ErrorContains(t, err, "type mismatch")
	})

	t.Run("preserves target interface when embedded method sets match", func(t *testing.T) {
		hookSrc := `package hooks

import "example.com/b"

type HookContext interface{}

func BeforeUse(ctx HookContext, x interface{ b.Marker }) {}
func AfterGet(ctx HookContext, r interface{ b.Marker }) {}
`
		ip, root, baseRule := setupCrossPkgInterfaceCase(t, methodPkgA, methodPkgB, targetSrc, hookSrc)

		beforeRule := *baseRule
		beforeRule.Func = "Use"
		beforeRule.Before = "BeforeUse"
		applyHookRule(t, ip, root, "Use", &beforeRule)

		afterRule := *baseRule
		afterRule.Func = "Get"
		afterRule.After = "AfterGet"
		applyHookRule(t, ip, root, "Get", &afterRule)

		src := renderFile(t, root)
		beforeStub := hookLinkName(beforeRule.Path, beforeRule.Before)
		afterStub := hookLinkName(afterRule.Path, afterRule.After)
		assert.Contains(t, src, "func "+beforeStub+"(hookContext OtelHookContext, param0 interface{ a.Marker })")
		assert.Contains(t, src, "func "+afterStub+"(hookContext OtelHookContext, arg0 interface{ a.Marker })")
	})

	t.Run("converts stub to empty interface when both embedded interfaces are empty", func(t *testing.T) {
		hookSrc := `package hooks

import "example.com/b"

type HookContext interface{}

func BeforeUse(ctx HookContext, x interface{ b.Marker }) {}
func AfterGet(ctx HookContext, r interface{ b.Marker }) {}
`
		ip, root, baseRule := setupCrossPkgInterfaceCase(t, emptyPkgA, emptyPkgB, targetSrc, hookSrc)

		beforeRule := *baseRule
		beforeRule.Func = "Use"
		beforeRule.Before = "BeforeUse"
		applyHookRule(t, ip, root, "Use", &beforeRule)

		afterRule := *baseRule
		afterRule.Func = "Get"
		afterRule.After = "AfterGet"
		applyHookRule(t, ip, root, "Get", &afterRule)

		src := renderFile(t, root)
		beforeStub := hookLinkName(beforeRule.Path, beforeRule.Before)
		afterStub := hookLinkName(afterRule.Path, afterRule.After)
		assert.Contains(t, src, "func "+beforeStub+"(hookContext OtelHookContext, param0 interface{})")
		assert.Contains(t, src, "func "+afterStub+"(hookContext OtelHookContext, arg0 interface{})")
	})

	t.Run("allows explicit any wildcard against non-empty embedded target interface", func(t *testing.T) {
		hookSrc := `package hooks

type HookContext interface{}

func BeforeUse(ctx HookContext, x any) {}
`
		ip, root, baseRule := setupCrossPkgInterfaceCase(t, methodPkgA, emptyPkgB, targetSrc, hookSrc)
		baseRule.Func = "Use"
		baseRule.Before = "BeforeUse"
		applyHookRule(t, ip, root, "Use", baseRule)

		src := renderFile(t, root)
		stub := hookLinkName(baseRule.Path, baseRule.Before)
		assert.Contains(t, src, "func "+stub+"(hookContext OtelHookContext, param0 interface{})")
	})

	t.Run("converts local empty wrapper embedding imported empty interface", func(t *testing.T) {
		hookSrc := `package hooks

import "example.com/b"

type HookContext interface{}
type Wrapper interface { b.Marker }

func BeforeUse(ctx HookContext, x interface{ Wrapper }) {}
`
		// Test pure local empty alias match against another empty interface.
		targetWrapperSrc := `package lib

import "example.com/a"

type Wrapper interface { a.Marker }

func Use(x interface{ Wrapper }) {}
`
		ip, root, baseRule := setupCrossPkgInterfaceCase(t, emptyPkgA, emptyPkgB, targetWrapperSrc, hookSrc)
		baseRule.Func = "Use"
		baseRule.Before = "BeforeUse"
		applyHookRule(t, ip, root, "Use", baseRule)

		src := renderFile(t, root)
		stub := hookLinkName(baseRule.Path, baseRule.Before)
		assert.Contains(t, src, "func "+stub+"(hookContext OtelHookContext, param0 interface{})")
	})
}

func TestEmbeddedInterfaceFallbackWithoutTargetTypes(t *testing.T) {
	hooksDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "hook.go"), []byte(`package hooks

type HookContext interface{}
type Marker interface{}

func BeforeUse(ctx HookContext, x interface{ Marker }) {}
func BeforeAnyEmbed(ctx HookContext, x interface{ any }) {}
`), 0o600))

	rLocalEmpty := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "local_empty"},
		Func:         "UseLocal",
		Before:       "BeforeUse",
		Path:         "example.com/app/hooks",
		ResolvedPath: hooksDir,
	}
	rAnyEmbed := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "any_embed"},
		Func:         "UseAny",
		Before:       "BeforeAnyEmbed",
		Path:         "example.com/app/hooks",
		ResolvedPath: hooksDir,
	}
	rExternalUnresolved := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "external_unresolved"},
		Func:         "UseExternal",
		Before:       "BeforeUse",
		Path:         "example.com/app/hooks",
		ResolvedPath: hooksDir,
	}

	root := parseFile(t, `package lib

import "example.com/a"

type Marker interface{}

func UseLocal(x interface{ Marker }) {}
func UseAny(x interface{ any }) {}
func UseExternal(x interface{ a.Marker }) {}
`)

	ip := newTestPhase()
	ip.target = root
	ip.parser = ast.NewAstParser()
	applyHookRule(t, ip, root, "UseLocal", rLocalEmpty)
	applyHookRule(t, ip, root, "UseAny", rAnyEmbed)

	src := renderFile(t, root)
	assert.Contains(
		t,
		src,
		"func "+hookLinkName(rLocalEmpty.Path, rLocalEmpty.Before)+"(hookContext OtelHookContext, param0 interface{})",
	)
	assert.Contains(
		t,
		src,
		"func "+hookLinkName(rAnyEmbed.Path, rAnyEmbed.Before)+"(hookContext OtelHookContext, param0 interface{})",
	)

	_, err := ip.applyFuncRule(
		t.Context(),
		rExternalUnresolved,
		root,
		findFuncDeclInFile(t, root, "UseExternal"),
		true,
	)
	require.ErrorContains(t, err, "cannot verify interface method sets")
	require.ErrorContains(t, err, "BeforeUse")
	require.ErrorContains(t, err, "param 1")
	require.ErrorContains(t, err, "target package type info unavailable")
}

func TestEmbeddedInterfaceUnresolvedHookError(t *testing.T) {
	dir := t.TempDir()
	aArchive := compileTestPackage(
		t,
		dir,
		"example.com/a",
		"package a\n\ntype Marker interface{ Tag() string }\n",
		map[string]string{},
	)
	targetPath := filepath.Join(dir, "lib.go")
	targetSrc := `package lib

import "example.com/a"

func Use(x interface{ a.Marker }) {}
func Get() interface{ a.Marker } { return nil }
`
	require.NoError(t, os.WriteFile(targetPath, []byte(targetSrc), 0o600))

	hooksDir := filepath.Join(dir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "hook.go"), []byte(`package hooks

import "example.com/unloadable/b"

type HookContext interface{}

func BeforeUse(ctx HookContext, x interface{ b.Marker }) {}
func AfterGet(ctx HookContext, x interface{ b.Marker }) {}
`), 0o600))

	ip := &instrumentPhase{
		logger:       slog.New(slog.DiscardHandler),
		compileArgs:  []string{"compile", "-p", "example.com/lib", targetPath},
		importConfig: imports.ImportConfig{PackageFile: map[string]string{"example.com/a": aArchive}},
	}
	root, err := ip.parseFile(targetPath)
	require.NoError(t, err)

	beforeRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "unloadable_before"},
		Func:         "Use",
		Before:       "BeforeUse",
		Path:         "example.com/hooks",
		ResolvedPath: hooksDir,
	}
	_, err = ip.applyFuncRule(t.Context(), beforeRule, root, findFuncDeclInFile(t, root, "Use"), true)
	require.ErrorContains(t, err, "cannot verify interface method sets")
	require.ErrorContains(t, err, "example.com/hooks.BeforeUse")
	require.ErrorContains(t, err, "param 1")
	require.ErrorContains(t, err, "example.com/unloadable/b")

	afterRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "unloadable_after"},
		Func:         "Get",
		After:        "AfterGet",
		Path:         "example.com/hooks",
		ResolvedPath: hooksDir,
	}
	_, err = ip.applyFuncRule(t.Context(), afterRule, root, findFuncDeclInFile(t, root, "Get"), true)
	require.ErrorContains(t, err, "cannot verify interface method sets")
	require.ErrorContains(t, err, "example.com/hooks.AfterGet")
	require.ErrorContains(t, err, "param 1")
}

func TestEmbeddedInterfaceMethodWithNamedTypeAcrossTypeGraphs(t *testing.T) {
	dir := t.TempDir()
	goMod := []byte("module example.com/mod\n\ngo 1.25.0\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), goMod, 0o600))
	for rel, src := range map[string]string{
		"shared/types.go": `package shared

type Value struct{ ID int }
type Box[T any] struct{ Item T }

type Marker interface {
	Mark(v Value, boxes ...Box[Value]) (int, error)
}
`,
		"other/types.go": `package other

import "example.com/mod/shared"

type Value struct{ ID int }

type Marker interface {
	Mark(v Value, boxes ...shared.Box[shared.Value]) (int, error)
}
`,
		"hooks/hook.go": `package hooks

import (
	"example.com/mod/other"
	"example.com/mod/shared"
)

type HookContext interface{}
type Marker interface {
	Mark(v shared.Value, boxes ...shared.Box[shared.Value]) (int, error)
}

func BeforeShared(ctx HookContext, x interface{ shared.Marker }) {}
func BeforeLocalMarker(ctx HookContext, x interface{ Marker }) {}
func BeforeOther(ctx HookContext, x interface{ other.Marker }) {}
`,
		"lib/lib.go": `package lib

import "example.com/mod/shared"

func UseShared(x interface{ shared.Marker }) {}
func UseWrapped(x interface{ shared.Marker }) {}
func UseOther(x interface{ shared.Marker }) {}
`,
	} {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	}

	// Deliberately omit other/types and use target importcfg only for shared so
	// target resolves via export data while hooks loads via packages.Load
	// (distinct go/types object instances for shared.Value).
	sharedArchive := compileTestPackage(
		t,
		dir,
		"example.com/mod/shared",
		"package shared\n\ntype Value struct{ ID int }\ntype Box[T any] struct{ Item T }\n"+
			"type Marker interface{ Mark(v Value, boxes ...Box[Value]) (int, error) }\n",
		map[string]string{},
	)
	targetPath := filepath.Join(dir, "lib", "lib.go")
	ip := &instrumentPhase{
		logger:       slog.New(slog.DiscardHandler),
		compileArgs:  []string{"compile", "-p", "example.com/mod/lib", targetPath},
		importConfig: imports.ImportConfig{PackageFile: map[string]string{"example.com/mod/shared": sharedArchive}},
	}
	root, err := ip.parseFile(targetPath)
	require.NoError(t, err)

	base := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "shared_named"},
		Path:         "example.com/mod/hooks",
		ResolvedPath: filepath.Join(dir, "hooks"),
	}

	rShared := *base
	rShared.Func = "UseShared"
	rShared.Before = "BeforeShared"
	applyHookRule(t, ip, root, "UseShared", &rShared)
	assert.Contains(
		t,
		renderFile(t, root),
		"func "+hookLinkName(rShared.Path, rShared.Before)+
			"(hookContext OtelHookContext, param0 interface{ shared.Marker })",
	)

	rWrapped := *base
	rWrapped.Func = "UseWrapped"
	rWrapped.Before = "BeforeLocalMarker"
	applyHookRule(t, ip, root, "UseWrapped", &rWrapped)
	assert.Contains(
		t,
		renderFile(t, root),
		"func "+hookLinkName(rWrapped.Path, rWrapped.Before)+
			"(hookContext OtelHookContext, param0 interface{ shared.Marker })",
	)

	rOther := *base
	rOther.Func = "UseOther"
	rOther.Before = "BeforeOther"
	_, err = ip.applyFuncRule(t.Context(), &rOther, root, findFuncDeclInFile(t, root, "UseOther"), true)
	require.ErrorContains(t, err, "type mismatch")
}

func TestEmbeddedInterfaceHookWithPackagesLoad(t *testing.T) {
	dir := t.TempDir()
	goMod := []byte("module example.com/mod\n\ngo 1.25.0\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), goMod, 0o600))
	hookSrc := "package hooks\n\nimport (\n\t\"example.com/mod/b\"\n\t\"example.com/mod/bmatch\"\n)\n\n" +
		"type HookContext interface{}\n\n" +
		"func BeforeMismatch(ctx HookContext, x interface{ b.Marker }) {}\n" +
		"func BeforeMatch(ctx HookContext, x interface{ bmatch.Marker }) {}\n"
	for rel, src := range map[string]string{
		"a/marker.go":      "package a\n\ntype Marker interface{ Mark() string }\n",
		"b/marker.go":      "package b\n\ntype Marker interface{}\n",
		"bmatch/marker.go": "package bmatch\n\ntype Marker interface{ Mark() string }\n",
		"hooks/hook.go":    hookSrc,
		"lib/lib.go":       "package lib\n\nimport \"example.com/mod/a\"\n\nfunc Use(x interface{ a.Marker }) {}\n",
	} {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	}

	targetPath := filepath.Join(dir, "lib", "lib.go")
	aArchive := compileTestPackage(
		t,
		dir,
		"example.com/mod/a",
		"package a\n\ntype Marker interface{ Mark() string }\n",
		map[string]string{},
	)
	ip := &instrumentPhase{
		logger:       slog.New(slog.DiscardHandler),
		compileArgs:  []string{"compile", "-p", "example.com/mod/lib", targetPath},
		importConfig: imports.ImportConfig{PackageFile: map[string]string{"example.com/mod/a": aArchive}},
	}
	root, err := ip.parseFile(targetPath)
	require.NoError(t, err)

	mismatchRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "mismatch"},
		Func:         "Use",
		Before:       "BeforeMismatch",
		Path:         "example.com/mod/hooks",
		ResolvedPath: filepath.Join(dir, "hooks"),
	}
	_, err = ip.applyFuncRule(t.Context(), mismatchRule, root, findFuncDeclInFile(t, root, "Use"), true)
	require.ErrorContains(t, err, "type mismatch")

	matchRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "match"},
		Func:         "Use",
		Before:       "BeforeMatch",
		Path:         "example.com/mod/hooks",
		ResolvedPath: filepath.Join(dir, "hooks"),
	}
	applyHookRule(t, ip, root, "Use", matchRule)
	src := renderFile(t, root)
	stub := hookLinkName(matchRule.Path, matchRule.Before)
	assert.Contains(t, src, "func "+stub+"(hookContext OtelHookContext, param0 interface{ a.Marker })")
}

func TestEmbeddedInterfaceMethodRecvVariadicAndAliases(t *testing.T) {
	const (
		pkgASrc     = "package a\n\ntype Marker interface{ Tag() string }\n"
		pkgBSrc     = "package b\n\ntype Marker interface{}\n"
		pkgMatchSrc = "package bmatch\n\ntype Marker interface{ Tag() string }\n"
	)

	targetSrc := `package lib

import "example.com/a"

type Service struct{}

func (s *Service) Handle(x interface{ a.Marker }) {}
func Variadic(xs ...interface{ a.Marker }) {}
`

	hookSrc := `package hooks

import (
	_ "unsafe"
	aliasb "example.com/b"
	aliasmatch "example.com/bmatch"
)

type HookContext interface{}
type Service struct{}

func BeforeHandle(ctx HookContext, s *Service, x interface{ aliasb.Marker }) {}
func BeforeHandleMatch(ctx HookContext, s *Service, x interface{ (aliasmatch.Marker) }) {}
func BeforeVariadic(ctx HookContext, xs ...interface{ aliasb.Marker }) {}
func BeforeVariadicMatch(ctx HookContext, xs ...interface{ aliasmatch.Marker }) {}
`

	dir := t.TempDir()
	archives := map[string]string{
		"example.com/a":      compileTestPackage(t, dir, "example.com/a", pkgASrc, map[string]string{}),
		"example.com/b":      compileTestPackage(t, dir, "example.com/b", pkgBSrc, map[string]string{}),
		"example.com/bmatch": compileTestPackage(t, dir, "example.com/bmatch", pkgMatchSrc, map[string]string{}),
	}
	targetPath := filepath.Join(dir, "lib.go")
	require.NoError(t, os.WriteFile(targetPath, []byte(targetSrc), 0o600))

	hooksDir := filepath.Join(dir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "hook.go"), []byte(hookSrc), 0o600))

	ip := &instrumentPhase{
		logger:       slog.New(slog.DiscardHandler),
		compileArgs:  []string{"compile", "-p", "example.com/lib", targetPath},
		importConfig: imports.ImportConfig{PackageFile: archives},
	}
	root, err := ip.parseFile(targetPath)
	require.NoError(t, err)
	baseRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "embedded_iface"},
		Path:         "example.com/hooks",
		ResolvedPath: hooksDir,
	}

	t.Run("receiver method slot resolves param index after receiver", func(t *testing.T) {
		rMismatch := *baseRule
		rMismatch.Func = "Handle"
		rMismatch.Recv = "*Service"
		rMismatch.Before = "BeforeHandle"
		_, applyErr := ip.applyFuncRule(
			t.Context(),
			&rMismatch,
			root,
			findFuncDeclInFile(t, root, "Handle"),
			true,
		)
		require.ErrorContains(t, applyErr, "type mismatch")

		rMatch := *baseRule
		rMatch.Func = "Handle"
		rMatch.Recv = "*Service"
		rMatch.Before = "BeforeHandleMatch"
		applyHookRule(t, ip, root, "Handle", &rMatch)
		src := renderFile(t, root)
		stub := hookLinkName(rMatch.Path, rMatch.Before)
		assert.Contains(
			t,
			src,
			"func "+stub+"(hookContext OtelHookContext, recv0 *Service, param0 interface{ a.Marker })",
		)
	})

	t.Run("variadic ellipsis unwraps slice element interface", func(t *testing.T) {
		rMismatch := *baseRule
		rMismatch.Func = "Variadic"
		rMismatch.Before = "BeforeVariadic"
		_, applyErr := ip.applyFuncRule(
			t.Context(),
			&rMismatch,
			root,
			findFuncDeclInFile(t, root, "Variadic"),
			true,
		)
		require.ErrorContains(t, applyErr, "type mismatch")

		rMatch := *baseRule
		rMatch.Func = "Variadic"
		rMatch.Before = "BeforeVariadicMatch"
		applyHookRule(t, ip, root, "Variadic", &rMatch)
		src := renderFile(t, root)
		stub := hookLinkName(rMatch.Path, rMatch.Before)
		assert.Contains(
			t,
			src,
			"func "+stub+"(hookContext OtelHookContext, param0 ...interface{ a.Marker })",
		)
	})
}

func TestMixedAndLocalASTInterfaceResolution(t *testing.T) {
	const pkgASrc = "package a\n\ntype Marker interface{ Tag() string }\n"

	dir := t.TempDir()
	aArchive := compileTestPackage(t, dir, "example.com/a", pkgASrc, map[string]string{})
	targetPath := filepath.Join(dir, "lib.go")
	targetSrc := `package lib

import "example.com/a"

type LocalMarker interface{ Tag() string }

func UseRemote(x interface{ a.Marker }) {}
func UseLocal(x interface{ LocalMarker }) {}
`
	require.NoError(t, os.WriteFile(targetPath, []byte(targetSrc), 0o600))

	hooksDir := filepath.Join(dir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "hook.go"), []byte(`package hooks

type HookContext interface{}
type Marker interface{}
type LocalEmpty interface{}
type LocalMarker interface{ Tag() string }
type LocalExplicit interface{ Do() string }

func BeforeMismatchEmpty(ctx HookContext, x interface{ Marker }) {}
func BeforeMismatchCount(ctx HookContext, x interface{ Marker; Tag() string }) {}
func BeforeAfterLocalEmpty(ctx HookContext, x interface{ LocalEmpty }) {}
func BeforeMatchAST(ctx HookContext, x interface{ LocalMarker }) {}
func BeforeExplicitAST(ctx HookContext, x interface{ LocalExplicit }) {}
func AfterLocal(ctx HookContext, x interface{ (LocalEmpty) }) {}
`), 0o600))

	ip := &instrumentPhase{
		logger:       slog.New(slog.DiscardHandler),
		compileArgs:  []string{"compile", "-p", "example.com/lib", targetPath},
		importConfig: imports.ImportConfig{PackageFile: map[string]string{"example.com/a": aArchive}},
	}
	root, err := ip.parseFile(targetPath)
	require.NoError(t, err)

	base := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "mixed"},
		Path:         "example.com/hooks",
		ResolvedPath: hooksDir,
	}

	// Target resolved via go/types (1 method), hook resolved via local AST (0 methods).
	rMixedMismatch := *base
	rMixedMismatch.Func = "UseRemote"
	rMixedMismatch.Before = "BeforeMismatchEmpty"
	_, err = ip.applyFuncRule(t.Context(), &rMixedMismatch, root, findFuncDeclInFile(t, root, "UseRemote"), true)
	require.ErrorContains(t, err, "type mismatch")

	// Pure AST target vs hook with same-named local embedded interface (Tag() string).
	astPhase := newTestPhase()
	astPhase.target = root
	astPhase.parser = ast.NewAstParser()

	rLocalMatch := *base
	rLocalMatch.Func = "UseLocal"
	rLocalMatch.Before = "BeforeMatchAST"
	applyHookRule(t, astPhase, root, "UseLocal", &rLocalMatch)
	assert.Contains(
		t,
		renderFile(t, root),
		"func "+hookLinkName(rLocalMatch.Path, rLocalMatch.Before)+
			"(hookContext OtelHookContext, param0 interface{ LocalMarker })",
	)
}

func TestInterfaceHelpersUnitBranches(t *testing.T) {
	t.Run("rsetHasEmbeddedInterfaceHook and hookHasEmbeddedInterface", func(t *testing.T) {
		hooksDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "hook.go"), []byte(`package hooks

type HookContext interface{}
type Marker interface{}

func BeforePlain(ctx HookContext, s string) {}
func BeforeEmbed(ctx HookContext, x interface{ Marker }) {}
func AfterVariadicEmbed(ctx HookContext, xs ...interface{ (Marker) }) {}
`), 0o600))

		ip := newTestPhase()
		rPlain := &rule.InstFuncRule{
			InstBaseRule: rule.InstBaseRule{Name: "plain"},
			Before:       "BeforePlain",
			Path:         "example.com/hooks",
			ResolvedPath: hooksDir,
		}
		rBeforeEmbed := &rule.InstFuncRule{
			InstBaseRule: rule.InstBaseRule{Name: "before_embed"},
			Before:       "BeforeEmbed",
			Path:         "example.com/hooks",
			ResolvedPath: hooksDir,
		}
		rAfterEmbed := &rule.InstFuncRule{
			InstBaseRule: rule.InstBaseRule{Name: "after_embed"},
			After:        "AfterVariadicEmbed",
			Path:         "example.com/hooks",
			ResolvedPath: hooksDir,
		}
		rMissing := &rule.InstFuncRule{
			InstBaseRule: rule.InstBaseRule{Name: "missing"},
			Before:       "DoesNotExist",
			Path:         "example.com/hooks",
			ResolvedPath: hooksDir,
		}

		rsetPlain := &rule.InstRuleSet{
			FuncRules: map[string][]*rule.InstFuncRule{"lib.go": {rPlain, rMissing}},
		}
		rsetBefore := &rule.InstRuleSet{
			FuncRules: map[string][]*rule.InstFuncRule{"lib.go": {rPlain, rBeforeEmbed}},
		}
		rsetAfter := &rule.InstRuleSet{
			FuncRules: map[string][]*rule.InstFuncRule{"lib.go": {rAfterEmbed}},
		}
		assert.False(t, ip.rsetHasEmbeddedInterfaceHook(rsetPlain))
		assert.True(t, ip.rsetHasEmbeddedInterfaceHook(rsetBefore))
		assert.True(t, ip.rsetHasEmbeddedInterfaceHook(rsetAfter))

		_, _, err := ip.resolveHookDeclAndFile(rMissing, true)
		require.Error(t, err)
	})

	t.Run("unwrap, AST cycles, selectors, and mixed comparisons", func(t *testing.T) {
		assert.False(t, hasEmbeddedInterface(nil))
		assert.False(t, hasExplicitInterfaceMethod(nil))
		assert.False(t, exprUsesExternalTypes(nil, nil, nil))
		assert.True(t, exprUsesExternalTypes(&dst.InterfaceType{
			Methods: &dst.FieldList{List: []*dst.Field{{Type: &dst.IndexExpr{X: ast.Ident("Generic")}}}},
		}, nil, map[string]bool{}))
		assert.Equal(t, "BeforeFn", hookDisplayName(hookDeclContext{
			funcDecl: &dst.FuncDecl{Name: ast.Ident("BeforeFn")},
		}))
		assert.Equal(t, "example.com/h.BeforeFn", hookDisplayName(hookDeclContext{
			rule:     &rule.InstFuncRule{Path: "example.com/h"},
			funcDecl: &dst.FuncDecl{Name: ast.Ident("BeforeFn")},
		}))

		assert.Equal(t, ast.Ident("string"), targetExprForCheck(ast.Ident("string"), nil, 0, false))
		arrayVsEllipsis := unwrapInterfaceSlotPair(
			&dst.ArrayType{Len: ast.IntLit(2), Elt: ast.InterfaceType()},
			&dst.Ellipsis{Elt: ast.InterfaceType()},
		)
		assert.False(t, arrayVsEllipsis.ok)
		assert.False(t, unwrapInterfaceSlotPair(ast.Ident("int"), &dst.Ellipsis{Elt: ast.InterfaceType()}).ok)
		assert.False(t, unwrapInterfaceSlotPair(&dst.ParenExpr{X: ast.Ident("int")}, ast.Ident("int")).ok)
		_, okParen := unwrapInterfaceExpr(&dst.ParenExpr{X: ast.Ident("int")})
		assert.False(t, okParen)

		cycleDecl := &dst.TypeSpec{
			Name: ast.Ident("Self"),
			Type: &dst.InterfaceType{Methods: &dst.FieldList{List: []*dst.Field{{Type: ast.Ident("Self")}}}},
		}
		nonIfaceDecl := &dst.TypeSpec{Name: ast.Ident("Num"), Type: ast.Ident("int")}
		decls := collectTypeDeclsFromFiles([]*dst.File{nil, {Decls: []dst.Decl{
			&dst.FuncDecl{Name: ast.Ident("fn"), Type: &dst.FuncType{}},
			&dst.GenDecl{Tok: token.TYPE, Specs: []dst.Spec{cycleDecl, nonIfaceDecl}},
		}}})
		assert.False(t, resolveASTInterface(ast.Ident("Self"), decls, map[string]bool{}).resolved)
		assert.False(t, resolveASTInterface(ast.Ident("Num"), decls, map[string]bool{}).resolved)
		assert.False(t, resolveASTInterface(&dst.ParenExpr{X: ast.Ident("Num")}, decls, map[string]bool{}).resolved)

		explicitWithVar := &dst.InterfaceType{Methods: &dst.FieldList{List: []*dst.Field{
			{Names: []*dst.Ident{ast.Ident("M")}, Type: &dst.FuncType{}},
			{Names: []*dst.Ident{ast.Ident("V")}, Type: ast.Ident("string")},
		}}}
		res := resolveASTInterface(explicitWithVar, decls, map[string]bool{})
		assert.True(t, res.resolved)
		assert.False(t, res.empty)
		assert.Equal(t, []string{"M()", "Vstring"}, res.methods)

		assert.Equal(
			t,
			ast.Ident("X"),
			unwrapEmbeddedTarget(&dst.IndexListExpr{X: &dst.IndexExpr{X: &dst.ParenExpr{X: ast.Ident("X")}}}),
		)

		pkg := types.NewPackage("example.com/root", "root")
		imp := types.NewPackage("example.com/other", "otherpkg")
		pkg.SetImports([]*types.Package{imp})
		file := &dst.File{
			Imports: []*dst.ImportSpec{{Path: &dst.BasicLit{Kind: token.STRING, Value: `"example.com/other"`}}},
		}
		assert.False(t, embeddedSelectorsValid(nil, file, nil))
		selMissing := &dst.SelectorExpr{X: ast.Ident("missing"), Sel: ast.Ident("M")}
		selCall := &dst.SelectorExpr{X: &dst.CallExpr{}, Sel: ast.Ident("M")}
		assert.False(t, embeddedExprValid(selMissing, file, pkg))
		assert.False(t, embeddedExprValid(selCall, file, pkg))
		assert.Nil(t, findImportedPackage(pkg, file, "unknown"))
		assert.Nil(t, extractTypesInterface(nil))

		emptyT := types.NewInterfaceType(nil, nil)
		nonEmptyT := types.NewInterfaceType([]*types.Func{
			types.NewFunc(token.NoPos, pkg, "Do", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		}, nil)
		otherNamedT := types.NewInterfaceType([]*types.Func{
			types.NewFunc(token.NoPos, pkg, "Other", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		}, nil)
		astNonEmpty := astInterfaceInfo{resolved: true, empty: false, methods: []string{"Do()"}}
		astTwoMethods := astInterfaceInfo{resolved: true, empty: false, methods: []string{"A()", "B()"}}
		assert.True(t, mixedInterfaceComparison(emptyT, astNonEmpty, "").known)
		assert.False(t, mixedInterfaceComparison(emptyT, astNonEmpty, "").equal)
		assert.False(t, mixedInterfaceComparison(nonEmptyT, astTwoMethods, "").equal)
		nonEmptyMixed := mixedInterfaceComparison(nonEmptyT, astNonEmpty, "missing load")
		assert.False(t, nonEmptyMixed.known)
		assert.Equal(t, "missing load", nonEmptyMixed.cause)
		assert.True(t, equalInterfaceMethodSets(
			resolvedInterface{astInfo: astNonEmpty},
			resolvedInterface{typesIface: emptyT},
		).known)
		assert.False(t, equalTypesInterfaces(nonEmptyT, emptyT))
		assert.False(t, equalTypesInterfaces(nonEmptyT, otherNamedT))
		assert.Equal(t, "uint8", canonicalTypeString(types.Typ[types.Byte]))
		assert.Equal(t, "int32", canonicalTypeString(types.Typ[types.Rune]))
		assert.Equal(t, "[2]chan<- int", canonicalTypeString(
			types.NewArray(types.NewChan(types.SendOnly, types.Typ[types.Int]), 2),
		))
		assert.Equal(t, "map[string]<-chan string", canonicalTypeString(
			types.NewMap(types.Typ[types.String], types.NewChan(types.RecvOnly, types.Typ[types.String])),
		))
		assert.Equal(t, "chan int", canonicalTypeString(
			types.NewChan(types.SendRecv, types.Typ[types.Int]),
		))
		nestedIface := types.NewInterfaceType([]*types.Func{
			types.NewFunc(token.NoPos, nil, "Cmp", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		}, []types.Type{types.Universe.Lookup("comparable").Type()})
		assert.Contains(t, canonicalTypeString(nestedIface), "comparable")
		assert.Equal(t, "struct{X int}", canonicalTypeString(types.NewStruct(
			[]*types.Var{types.NewField(token.NoPos, nil, "X", types.Typ[types.Int], false)},
			nil,
		)))
		assert.Empty(t, packagePathQualifier(nil))

		imp.Scope().Insert(types.NewTypeName(token.NoPos, imp, "M", emptyT))
		aliasFile := &dst.File{
			Imports: []*dst.ImportSpec{{
				Name: ast.Ident("otheralias"),
				Path: &dst.BasicLit{Kind: token.STRING, Value: `"example.com/other"`},
			}},
		}
		assert.True(t, embeddedExprValid(
			&dst.SelectorExpr{X: ast.Ident("otheralias"), Sel: ast.Ident("M")},
			aliasFile,
			pkg,
		))

		_, ok := targetSlotType(nil, nil, 0, true)
		assert.False(t, ok)
		sigZero := types.NewSignatureType(nil, nil, nil, nil, nil, false)
		fnNoRecv := &dst.FuncDecl{Name: ast.Ident("F"), Type: &dst.FuncType{}}
		_, ok = targetSlotType(sigZero, fnNoRecv, 0, false)
		assert.False(t, ok)
		_, ok = targetSlotType(sigZero, fnNoRecv, 0, true)
		assert.False(t, ok)
		recvStruct := types.NewNamed(
			types.NewTypeName(token.NoPos, pkg, "S", nil),
			types.NewStruct(nil, nil),
			nil,
		)
		sigWithRecv := types.NewSignatureType(
			types.NewVar(token.NoPos, pkg, "s", types.NewPointer(recvStruct)),
			nil,
			nil,
			nil,
			nil,
			false,
		)
		fnWithRecv := &dst.FuncDecl{
			Recv: &dst.FieldList{List: []*dst.Field{{Type: ast.Ident("S")}}},
			Name: ast.Ident("M"),
			Type: &dst.FuncType{},
		}
		_, ok = targetSlotType(sigZero, fnWithRecv, 0, true)
		assert.False(t, ok)
		recvTy, ok := targetSlotType(sigWithRecv, fnWithRecv, 0, true)
		assert.True(t, ok)
		assert.NotNil(t, recvTy)

		ip := newTestPhase()
		assert.Empty(t, hookCacheKey(nil))
		assert.Nil(t, ip.collectHookFiles(nil, nil))
		assert.Len(t, ip.collectHookFiles(nil, file), 1)
		missingRule := &rule.InstFuncRule{ResolvedPath: filepath.Join(t.TempDir(), "missing")}
		assert.Nil(t, ip.collectHookFiles(missingRule, nil))
		assert.Len(t, ip.collectHookFiles(missingRule, file), 1)
		assert.False(t, ip.hookHasEmbeddedInterface(missingRule, true))
		badPkgRule := &rule.InstFuncRule{Path: "example.invalid/missing/pkg"}
		loadedPkg, loadCause := ip.loadHookGoTypes(t.Context(), badPkgRule, nil)
		assert.Nil(t, loadedPkg)
		assert.NotEmpty(t, loadCause)
		emptyPkg, emptyCause := ip.loadHookGoTypes(t.Context(), &rule.InstFuncRule{}, nil)
		assert.Nil(t, emptyPkg)
		assert.Contains(t, emptyCause, "target import config unavailable")
		assert.Equal(t, "interface type resolution failed", firstCause(""))
		_ = hookDirBuildFlags()

		_, _, declErr := ip.resolveHookDeclAndFile(&rule.InstFuncRule{
			Before:       "F",
			ResolvedPath: brokenHookSyntaxDir(t),
		}, true)
		require.Error(t, declErr)
		badSyntaxFiles := ip.collectHookFiles(&rule.InstFuncRule{ResolvedPath: brokenHookSyntaxDir(t)}, nil)
		assert.Empty(t, badSyntaxFiles)

		resMissingHook := ip.resolveHookInterface(t.Context(), hookDeclContext{}, 1, explicitWithVar)
		assert.Empty(t, resMissingHook.cause)
		resExtNoRule := ip.resolveHookInterface(
			t.Context(),
			hookDeclContext{},
			1,
			&dst.InterfaceType{Methods: &dst.FieldList{List: []*dst.Field{
				{Type: &dst.SelectorExpr{X: ast.Ident("a"), Sel: ast.Ident("Marker")}},
			}}},
		)
		assert.Contains(t, resExtNoRule.cause, "missing hook rule")

		// Exercise tryCheckHookFromExportConfig and resolveHookInterface error branches.
		hookPkgDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(hookPkgDir, "hook.go"), []byte(
			"package hookpkg\n\nfunc Before(ctx interface{}, x int) {}\n",
		), 0o600))
		phaseNoCfg := newTestPhase()
		_, noCfgErr := phaseNoCfg.tryCheckHookFromExportConfig(
			t.Context(),
			&rule.InstFuncRule{ResolvedPath: hookPkgDir},
			nil,
		)
		require.ErrorContains(t, noCfgErr, "target import config unavailable")

		fakeToolDir := t.TempDir()
		exportName := "export"
		if filepath.Ext(os.Args[0]) == ".exe" {
			exportName += ".exe"
		}
		require.NoError(t, os.WriteFile(filepath.Join(fakeToolDir, exportName), nil, 0o600))
		phaseWithExport := &instrumentPhase{
			logger:       slog.New(slog.DiscardHandler),
			compileArgs:  []string{filepath.Join(fakeToolDir, "compile"), "-p", "example.com/lib"},
			importConfig: imports.ImportConfig{PackageFile: map[string]string{"example.com/a": "/tmp/a.a"}},
			resolveExportFiles: func(context.Context, []string, ...string) (map[string]string, map[string]error, error) {
				return nil, nil, errors.New("export failed")
			},
		}
		_, exportErr := phaseWithExport.tryCheckHookFromExportConfig(
			t.Context(),
			&rule.InstFuncRule{ResolvedPath: hookPkgDir},
			nil,
		)
		require.ErrorContains(t, exportErr, "export failed")

		phaseWithExport.compileArgs = []string{"compile", "-p", "example.com/lib"}
		phaseWithExport.importConfig.PackageFile = map[string]string{"unsafe": ""}
		_, exportErr = phaseWithExport.tryCheckHookFromExportConfig(
			t.Context(),
			&rule.InstFuncRule{ResolvedPath: hookPkgDir},
			[]*dst.File{file},
		)
		require.ErrorContains(t, exportErr, "missing from target import config")

		_, noFilesErr := phaseWithExport.tryCheckHookFromExportConfig(
			t.Context(),
			&rule.InstFuncRule{ResolvedPath: filepath.Join(t.TempDir(), "empty")},
			nil,
		)
		require.ErrorContains(t, noFilesErr, "no hook package files found")

		_, typeCheckedPkgErr := phaseWithExport.tryCheckHookFromExportConfig(
			t.Context(),
			&rule.InstFuncRule{ResolvedPath: hookPkgDir},
			[]*dst.File{parseFile(t, "package hookpkg\n\nimport _ \"unsafe\"\n")},
		)
		require.NoError(t, typeCheckedPkgErr)

		_, missingExtractErr := extractLoadedPackage(nil, "example.com/x")
		require.Error(t, missingExtractErr)
		_, errExtract := extractLoadedPackage([]*packages.Package{{
			Errors: []packages.Error{{Msg: "broken"}},
		}}, "example.com/x")
		require.ErrorContains(t, errExtract, "broken")

		hookIP := &instrumentPhase{
			logger:       slog.New(slog.DiscardHandler),
			compileArgs:  []string{"compile", "-p", "example.com/lib", filepath.Join(t.TempDir(), "missing.go")},
			targetFunc:   &dst.FuncDecl{Name: ast.Ident("Fn")},
			importConfig: imports.ImportConfig{PackageFile: map[string]string{"example.com/a": "/tmp/a.a"}},
		}
		hookValidDecl := &dst.FuncDecl{Name: ast.Ident("Before")}
		hookMissingDecl := &dst.FuncDecl{Name: ast.Ident("Missing")}
		extIface := &dst.InterfaceType{Methods: &dst.FieldList{List: []*dst.Field{
			{Type: &dst.SelectorExpr{X: ast.Ident("other"), Sel: ast.Ident("Marker")}},
		}}}
		badSelRes := hookIP.resolveHookInterface(t.Context(), hookDeclContext{
			rule:     &rule.InstFuncRule{Path: "hookpkg", ResolvedPath: hookPkgDir},
			file:     parseFile(t, "package hookpkg\n"),
			funcDecl: hookValidDecl,
		}, 1, extIface)
		assert.Contains(t, badSelRes.cause, "unresolved hook embedded interface")

		missingFnRes := hookIP.resolveHookInterface(t.Context(), hookDeclContext{
			rule:     &rule.InstFuncRule{Path: "hookpkg", ResolvedPath: hookPkgDir},
			file:     parseFile(t, "package hookpkg\n"),
			funcDecl: hookMissingDecl,
		}, 1, explicitWithVar)
		assert.Contains(t, missingFnRes.cause, "not found in loaded package")

		outOfRangeRes := hookIP.resolveHookInterface(t.Context(), hookDeclContext{
			rule:     &rule.InstFuncRule{Path: "hookpkg", ResolvedPath: hookPkgDir},
			file:     parseFile(t, "package hookpkg\n"),
			funcDecl: hookValidDecl,
		}, 9, explicitWithVar)
		assert.Contains(t, outOfRangeRes.cause, "parameter unavailable")

		nonIfaceRes := hookIP.resolveHookInterface(t.Context(), hookDeclContext{
			rule:     &rule.InstFuncRule{Path: "hookpkg", ResolvedPath: hookPkgDir},
			file:     parseFile(t, "package hookpkg\n"),
			funcDecl: hookValidDecl,
		}, 1, explicitWithVar)
		assert.Contains(t, nonIfaceRes.cause, "did not resolve to an interface")

		targetFailRes := hookIP.resolveTargetInterface(t.Context(), 1, true, explicitWithVar)
		assert.Contains(t, targetFailRes.cause, "target package type check failed")
	})
}

func brokenHookSyntaxDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package hook\nfunc {"), 0o600))
	return dir
}
