// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackageGoFiles(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package pkg\n"), 0o644))
	missingGoFile := filepath.Join(dir, "nonexistent.go")

	ip := newTestPhase()
	ip.compileArgs = []string{
		"-p", "example.com/pkg",
		"-o", filepath.Join(dir, "pkg.a"),
		goFile,
		missingGoFile,
		filepath.Join(dir, "pkg.a"), // not a .go file, must be excluded
	}

	files := ip.packageGoFiles()

	abs, err := filepath.Abs(goFile)
	require.NoError(t, err)
	assert.Equal(t, []string{abs}, files, "only the existing .go file should be kept")
}

// TestLoadSiblingASTs_ParsesEveryPackageFile confirms loadSiblingASTs caches
// the WHOLE package's file set, including ip.targetPath's own file -- it must
// not pre-exclude anything at build time. Exclusion of the current file
// happens later, in resolveGenericTypeDecl, at lookup time; see
// TestResolveGenericTypeDecl_FindsDeclarationInEarlierProcessedFile for why
// that split matters (excluding at build time made the cache permanently
// blind to whichever file was "current" when it was first populated).
func TestLoadSiblingASTs_ParsesEveryPackageFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.go")
	sibling := filepath.Join(dir, "sibling.go")
	require.NoError(t, os.WriteFile(target, []byte("package pkg\n"), 0o644))
	require.NoError(t, os.WriteFile(sibling, []byte("package pkg\n\ntype Box[T any] struct{}\n"), 0o644))

	absTarget, err := filepath.Abs(target)
	require.NoError(t, err)
	absSibling, err := filepath.Abs(sibling)
	require.NoError(t, err)

	ip := newTestPhase()
	ip.compileArgs = []string{target, sibling}
	ip.targetPath = absTarget

	siblings := ip.loadSiblingASTs()

	_, hasTarget := siblings[absTarget]
	assert.True(t, hasTarget, "loadSiblingASTs must cache the target's own file too, not just its siblings")
	_, hasSibling := siblings[absSibling]
	assert.True(t, hasSibling, "a real sibling file should be parsed and present")
}

func TestLoadSiblingASTs_SkipsUnparsableFile(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.go")
	broken := filepath.Join(dir, "broken.go")
	require.NoError(t, os.WriteFile(valid, []byte("package pkg\n"), 0o644))
	require.NoError(t, os.WriteFile(broken, []byte("package pkg\n\nfunc ( {{{ this is not valid go\n"), 0o644))

	absValid, err := filepath.Abs(valid)
	require.NoError(t, err)
	absBroken, err := filepath.Abs(broken)
	require.NoError(t, err)

	ip := newTestPhase()
	ip.compileArgs = []string{valid, broken}
	ip.targetPath = "" // neither file is "current" for this test

	siblings := ip.loadSiblingASTs()

	_, hasValid := siblings[absValid]
	assert.True(t, hasValid, "the valid file should still be parsed and cached")
	_, hasBroken := siblings[absBroken]
	assert.False(t, hasBroken, "a file that fails to parse must be skipped, not fail the whole call")
}
