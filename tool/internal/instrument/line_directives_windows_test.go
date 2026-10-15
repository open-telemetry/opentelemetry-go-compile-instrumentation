// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package instrument

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedFileWindows(t *testing.T) {
	// The name is at the root of the volume of the file, so it is absolute,
	// and Go does not join it with the directory of the file.
	tests := map[string]string{
		`C:\work\dir\main.go`:        `C:\<generated>`,
		`D:\main.go`:                 `D:\<generated>`,
		`\\server\share\dir\main.go`: `\\server\share\<generated>`,
		`\\?\C:\work\dir\main.go`:    `\\?\C:\<generated>`,
		// filepath.VolumeName gives only `\\?\UNC` for this form. The name
		// is still absolute.
		`\\?\UNC\server\share\x\main.go`: `\\?\UNC\<generated>`,
	}
	for file, want := range tests {
		got := generatedFile(file)
		assert.Equal(t, want, got, file)
		assert.True(t, filepath.IsAbs(got), got)
	}
}

func TestGeneratedFileWindowsRuntimeCaller(t *testing.T) {
	// The Go compiler on Windows reports code after the directive in the
	// generated file, with no directory before it.
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	name := generatedFile(file)
	src := "package main\n\nimport (\n\t\"fmt\"\n\t\"runtime\"\n)\n\nfunc main() {\n" +
		"\t/*line " + name + ":1*/ _, f, l, _ := runtime.Caller(0)\n" +
		"\tfmt.Printf(\"%s:%d\\n\", f, l)\n}\n"
	require.NoError(t, os.WriteFile(file, []byte(src), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o600))
	cmd := exec.Command("go", "run", "main.go")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	got := strings.TrimSpace(string(out))
	// The runtime can use "/" as separator. Compare with one separator.
	assert.Equal(t, filepath.ToSlash(name)+":1", filepath.ToSlash(got))
}
