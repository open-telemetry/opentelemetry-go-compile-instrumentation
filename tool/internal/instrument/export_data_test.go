// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"context"
	"errors"
	goast "go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/gcexportdata"

	"go.opentelemetry.io/otelc/tool/internal/imports"
)

const exportDataTestSource = `package sample

import "example.com/dep"

func f() { dep.T{}.M() }
`

// newExportDataPhase returns a phase compiling exportDataTestSource with a
// toolchain that ships cmd/export. The importcfg points dep at a missing
// archive, so dep.T only resolves through go list -export results.
func newExportDataPhase(t *testing.T, logs *bytes.Buffer) (*instrumentPhase, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "export"), nil, 0o755))
	src := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(src, []byte(exportDataTestSource), 0o600))

	ip := &instrumentPhase{
		logger:      slog.New(slog.NewTextHandler(logs, nil)),
		compileArgs: []string{filepath.Join(dir, "compile"), "-p", "example.com/sample", src},
		importConfig: imports.ImportConfig{PackageFile: map[string]string{
			"example.com/dep": filepath.Join(dir, "missing.a"),
		}},
	}
	return ip, dir
}

// writeDepExportFile writes export data for a package example.com/dep
// declaring T with a method M, in the format cmd/export produces.
func writeDepExportFile(t *testing.T, dir string) string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "dep.go", "package dep\n\ntype T struct{}\n\nfunc (T) M() {}\n", 0)
	require.NoError(t, err)
	pkg, err := (&types.Config{}).Check("example.com/dep", fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	var data bytes.Buffer
	require.NoError(t, gcexportdata.Write(&data, fset, pkg))
	path := filepath.Join(dir, "dep")
	require.NoError(t, os.WriteFile(path, data.Bytes(), 0o600))
	return path
}

func TestEnsureMethodCallInfo_ExportTool_UsesGoListFiles(t *testing.T) {
	ip, dir := newExportDataPhase(t, &bytes.Buffer{})
	depFile := writeDepExportFile(t, dir)
	for _, p := range []string{"example.com/sample_test", "unsafe", "C"} {
		ip.importConfig.PackageFile[p] = filepath.Join(dir, "unused.a")
	}
	ip.resolveExportFiles = func(_ context.Context, paths []string, _ ...string) (map[string]string, map[string]error, error) {
		assert.Equal(t, []string{"example.com/dep", "example.com/sample_test"}, paths)
		return map[string]string{"example.com/dep": depFile},
			map[string]error{"example.com/sample_test": errors.New("cannot load")}, nil
	}

	info := ip.ensureMethodCallInfo(t.Context())

	require.NotNil(t, info)
	line := strings.Split(exportDataTestSource, "\n")[4]
	path, recv, ok := info.methodReceiver("sample.go", 5, strings.Index(line, "M()")+1)
	assert.True(t, ok)
	assert.Equal(t, "example.com/dep", path)
	assert.Equal(t, "T", recv)
}

func TestEnsureMethodCallInfo_ExportTool_ResolveError(t *testing.T) {
	var logs bytes.Buffer
	ip, _ := newExportDataPhase(t, &logs)
	ip.resolveExportFiles = func(context.Context, []string, ...string) (map[string]string, map[string]error, error) {
		return nil, nil, errors.New("boom")
	}

	assert.Nil(t, ip.ensureMethodCallInfo(t.Context()))
	assert.Contains(t, logs.String(), "boom")
}
