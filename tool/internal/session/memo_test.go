// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

// setupSession points this process at a fresh work dir and session, records
// "scope1" as the resolution scope.
func setupSession(t *testing.T) {
	t.Helper()
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcSessionDir, filepath.Join(t.TempDir(), "session"))
	writeScope(t, "scope1")
}

func writeScope(t *testing.T, scope string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetResolutionScopeFile(), []byte(scope), 0o644))
}

// archive creates a fake export file and returns its path.
func archive(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name+".a")
	require.NoError(t, os.WriteFile(p, []byte(name), 0o644))
	return p
}

func TestMemoHitAndMiss(t *testing.T) {
	setupSession(t)
	m := Open([]string{"-tags=x"})
	require.NotNil(t, m)

	_, _, ok := m.Lookup("example.com/a", nil)
	assert.False(t, ok, "an empty memo has no entry")

	closure := map[string]string{"example.com/a": archive(t, "a"), "fmt": archive(t, "fmt")}
	m.Store("example.com/a", closure, nil)

	got, failed, ok := Open([]string{"-tags=x"}).Lookup("example.com/a", map[string]string{"fmt": closure["fmt"]})
	require.True(t, ok)
	assert.Empty(t, failed)
	assert.Equal(t, closure, got)
}

func TestMemoSeparatesScopesAndFlags(t *testing.T) {
	setupSession(t)
	Open(nil).Store("example.com/a", map[string]string{"example.com/a": archive(t, "a")}, nil)

	_, _, ok := Open([]string{"-tags=y"}).Lookup("example.com/a", nil)
	assert.False(t, ok, "other build flags must not reuse the entry")

	writeScope(t, "scope2")
	_, _, ok = Open(nil).Lookup("example.com/a", nil)
	assert.False(t, ok, "another resolution scope must not reuse the entry")
}

func TestMemoPGOProfileContentIsPartOfFlags(t *testing.T) {
	setupSession(t)
	profile := filepath.Join(t.TempDir(), "default.pgo")
	require.NoError(t, os.WriteFile(profile, []byte("v1"), 0o644))
	flags := []string{"-pgo=" + profile}
	Open(flags).Store("example.com/a", map[string]string{"example.com/a": archive(t, "a")}, nil)

	_, _, ok := Open(flags).Lookup("example.com/a", nil)
	require.True(t, ok)

	require.NoError(t, os.WriteFile(profile, []byte("v2"), 0o644))
	_, _, ok = Open(flags).Lookup("example.com/a", nil)
	assert.False(t, ok, "an edited PGO profile must not reuse the entry")
}

func TestMemoRejectsStaleArchive(t *testing.T) {
	setupSession(t)
	a := archive(t, "a")
	Open(nil).Store("example.com/a", map[string]string{"example.com/a": a}, nil)
	require.NoError(t, os.Remove(a))

	_, _, ok := Open(nil).Lookup("example.com/a", nil)
	assert.False(t, ok, "an entry whose archive is gone must be a miss")
}

func TestMemoRejectsOverlapMismatch(t *testing.T) {
	setupSession(t)
	closure := map[string]string{"example.com/a": archive(t, "a"), "fmt": archive(t, "fmt")}
	Open(nil).Store("example.com/a", closure, nil)

	_, _, ok := Open(nil).Lookup("example.com/a", map[string]string{"fmt": archive(t, "otherfmt")})
	assert.False(t, ok, "an entry resolved against other deps must be a miss")

	// An archive the build compiled itself in its $WORK is compared like any
	// other: a different path is a miss, even when it is named _pkg_.a.
	_, _, ok = Open(nil).Lookup("example.com/a",
		map[string]string{"fmt": filepath.Join(t.TempDir(), "b001", "_pkg_.a")})
	assert.False(t, ok, "a differing _pkg_.a mapping must be a miss")

	// The same archive the entry recorded is a hit.
	_, _, ok = Open(nil).Lookup("example.com/a", map[string]string{"fmt": closure["fmt"]})
	assert.True(t, ok)
}

func TestMemoNegativeEntry(t *testing.T) {
	setupSession(t)
	Open(nil).Store("example.com/bad", nil, errors.New("no such package"))

	_, failed, ok := Open(nil).Lookup("example.com/bad", nil)
	require.True(t, ok)
	assert.Equal(t, "no such package", failed)
}

func TestMemoDisabled(t *testing.T) {
	setupSession(t)
	t.Setenv(util.EnvOtelcSessionDir, util.SessionOff)
	assert.Nil(t, Open(nil))

	t.Setenv(util.EnvOtelcSessionDir, t.TempDir())
	require.NoError(t, os.Remove(util.GetResolutionScopeFile()))
	assert.Nil(t, Open(nil), "without a recorded scope the memo is off")

	var m *Memo
	m.Store("x", map[string]string{"x": "y"}, nil)
	_, _, ok := m.Lookup("x", nil)
	assert.False(t, ok)
}

func TestMemoConcurrentWriters(t *testing.T) {
	setupSession(t)
	closure := map[string]string{"example.com/a": archive(t, "a")}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			Open(nil).Store("example.com/a", closure, nil)
			// Every reader must see either no entry or a complete one.
			if got, _, ok := Open(nil).Lookup("example.com/a", nil); ok {
				assert.Equal(t, closure, got)
			}
		})
	}
	wg.Wait()
	got, _, ok := Open(nil).Lookup("example.com/a", nil)
	require.True(t, ok)
	assert.Equal(t, closure, got)
}

func TestMemoLockSerializes(t *testing.T) {
	setupSession(t)
	m := Open(nil)
	var inside, overlaps atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			release, err := m.Lock(t.Context())
			if err != nil {
				t.Error(err)
				return
			}
			if inside.Add(1) > 1 {
				overlaps.Add(1)
			}
			time.Sleep(5 * time.Millisecond)
			inside.Add(-1)
			release()
		})
	}
	wg.Wait()
	assert.Zero(t, overlaps.Load())
}

func TestMemoLockCanceled(t *testing.T) {
	setupSession(t)
	m := Open(nil)
	release, err := m.Lock(t.Context())
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = m.Lock(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
