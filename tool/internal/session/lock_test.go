// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestLockPathFailsOnPermanentError(t *testing.T) {
	// A lock file that cannot be created in a read-only directory never
	// becomes available, so the lock must fail instead of polling forever.
	dir := readOnlyDir(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err := lockPath(ctx, filepath.Join(dir, "busy.lock"), nil)
	require.Error(t, err)
	assert.NotErrorIs(t, err, context.DeadlineExceeded, "a permanent failure must not wait for the deadline")
}

func TestWithLoadSlotFailsOnPermanentError(t *testing.T) {
	session := t.TempDir()
	t.Setenv(util.EnvOtelcSessionDir, session)
	t.Setenv(util.EnvOtelcMaxParallelLoads, "1")
	slots := filepath.Join(session, slotsDir)
	require.NoError(t, os.MkdirAll(slots, 0o755))
	makeReadOnly(t, slots)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	err := WithLoadSlot(ctx, func(context.Context) error {
		t.Error("ran without a slot")
		return nil
	})
	require.Error(t, err)
	assert.NotErrorIs(t, err, context.DeadlineExceeded)
}

// readOnlyDir returns an empty directory no lock file can be created in.
func readOnlyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	makeReadOnly(t, dir)
	return dir
}

func makeReadOnly(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not stop file creation here")
	}
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// slotHelperEnv makes the test binary act as a slot-holding helper process.
const slotHelperEnv = "OTELC_SESSION_SLOT_HELPER"

// TestSlotHelperProcess is the helper body: it takes a load slot, records
// when it held it, and holds it for a while. It does nothing in a normal run.
func TestSlotHelperProcess(t *testing.T) {
	out := os.Getenv(slotHelperEnv)
	if out == "" {
		t.Skip("helper process only")
	}
	err := WithLoadSlot(context.Background(), func(context.Context) error {
		start := time.Now().UnixNano()
		time.Sleep(300 * time.Millisecond)
		end := time.Now().UnixNano()
		return os.WriteFile(out, []byte(strconv.FormatInt(start, 10)+" "+strconv.FormatInt(end, 10)), 0o644)
	})
	require.NoError(t, err)
}

func TestWithLoadSlotBoundsAcrossProcesses(t *testing.T) {
	session := t.TempDir()
	outDir := t.TempDir()
	const helpers = 2
	cmds := make([]*exec.Cmd, helpers)
	for i := range helpers {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSlotHelperProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			util.EnvOtelcSessionDir+"="+session,
			util.EnvOtelcMaxParallelLoads+"=1",
			slotHelperEnv+"="+filepath.Join(outDir, strconv.Itoa(i)),
		)
		require.NoError(t, cmd.Start())
		cmds[i] = cmd
	}
	for _, cmd := range cmds {
		require.NoError(t, cmd.Wait())
	}

	type span struct{ start, end int64 }
	spans := make([]span, helpers)
	for i := range helpers {
		content, err := os.ReadFile(filepath.Join(outDir, strconv.Itoa(i)))
		require.NoError(t, err)
		fields := strings.Fields(string(content))
		require.Len(t, fields, 2)
		spans[i].start, err = strconv.ParseInt(fields[0], 10, 64)
		require.NoError(t, err)
		spans[i].end, err = strconv.ParseInt(fields[1], 10, 64)
		require.NoError(t, err)
	}
	a, b := spans[0], spans[1]
	assert.True(t, a.end <= b.start || b.end <= a.start,
		"two processes held the only load slot at once: %v %v", a, b)
}

func TestLockPathReportsWaitOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.lock")
	release, err := lockPath(t.Context(), path, nil)
	require.NoError(t, err)

	waits := 0
	acquired := make(chan func())
	go func() {
		next, lockErr := lockPath(context.Background(), path, func() { waits++ })
		assert.NoError(t, lockErr)
		acquired <- next
	}()
	time.Sleep(3 * lockRetryInterval)
	release()
	next := <-acquired
	next()
	assert.Equal(t, 1, waits, "onWait must run exactly once while the lock is busy")
}

// TestLockWaitBacksOff pins the cost of a long wait: attempts that fail
// reopen the lock file, so their rate must decay instead of staying at one
// per lockRetryInterval.
func TestLockWaitBacksOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.lock")
	release, err := lockPath(t.Context(), path, nil)
	require.NoError(t, err)

	const held = 2 * time.Second
	b := newPollBackoff()
	done := make(chan error)
	go func() {
		next, lockErr := lockPathWith(context.Background(), path, nil, b)
		if lockErr == nil {
			next()
		}
		done <- lockErr
	}()
	time.Sleep(held)
	release()
	require.NoError(t, <-done)

	fixedRate := int(held / lockRetryInterval)
	assert.LessOrEqual(t, b.polls, 6, "a %v wait made %d attempts; a fixed interval makes about %d",
		held, b.polls, fixedRate)
	assert.Positive(t, b.polls)
}

func TestSlotWaitBacksOff(t *testing.T) {
	dir := t.TempDir()
	release, _, err := acquireSlot(t.Context(), dir, 1, newPollBackoff())
	require.NoError(t, err)

	const held = 2 * time.Second
	b := newPollBackoff()
	done := make(chan error)
	go func() {
		next, _, slotErr := acquireSlot(context.Background(), dir, 1, b)
		if slotErr == nil {
			next()
		}
		done <- slotErr
	}()
	time.Sleep(held)
	release()
	require.NoError(t, <-done)
	assert.LessOrEqual(t, b.polls, 6, "a %v slot wait made %d rounds of attempts", held, b.polls)
	assert.Positive(t, b.polls)
}

func TestPollBackoffDoublesUpToTheCap(t *testing.T) {
	b := &pollBackoff{delay: time.Millisecond}
	const attempts = 12
	delays := make([]time.Duration, 0, attempts)
	for range attempts {
		delays = append(delays, b.delay)
		require.NoError(t, b.wait(t.Context()))
	}
	for i := 1; i < len(delays); i++ {
		assert.Equal(t, min(2*delays[i-1], maxLockRetryInterval), delays[i])
	}
	assert.Equal(t, attempts, b.polls)
}
