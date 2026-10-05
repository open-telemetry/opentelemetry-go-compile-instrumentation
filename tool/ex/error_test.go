// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ex

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestError(t *testing.T) {
	err := Newf("a")
	err = Wrapf(err, "b")
	err = Wrap(Wrap(Wrap(err))) // make no sense
	require.Contains(t, err.Error(), "a")
	require.Contains(t, err.Error(), "b")

	err = errors.New("c")
	err = Wrapf(err, "d")
	err = Wrapf(err, "e")
	err = Wrap(Wrap(Wrap(err))) // make no sense
	require.Contains(t, err.Error(), "c")
	require.Contains(t, err.Error(), "d")
}

func TestJoinStackful(t *testing.T) {
	e1 := New("first")
	e2 := Newf("second %d", 2)
	joined := Join(e1, e2)

	require.ErrorIs(t, joined, e1)
	require.ErrorIs(t, joined, e2)

	var se *stackfulError
	require.ErrorAs(t, joined, &se)
}

func TestJoinMixed(t *testing.T) {
	stdErr := errors.New("std")
	exErr := New("ex")
	joined := Join(stdErr, exErr)

	require.ErrorIs(t, joined, stdErr)
	require.ErrorIs(t, joined, exErr)

	var se *stackfulError
	require.ErrorAs(t, joined, &se)
	require.Contains(t, se.Error(), "ex")
}

// captureStderr redirects os.Stderr for the duration of fn and returns whatever
// was written. printError writes directly to os.Stderr, so this lets us assert
// on its output without spawning a subprocess.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w                       //nolint:reassign // printError writes to os.Stderr; redirect it to capture output
	defer func() { os.Stderr = orig }() //nolint:reassign // restore the original os.Stderr

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()
	require.NoError(t, w.Close())
	return <-done
}

func TestPrintErrorStackful(t *testing.T) {
	out := captureStderr(t, func() {
		printError(Newf("stackful boom %d", 42))
	})
	// Stackful errors print the message list and a stack trace section.
	assert.Contains(t, out, "stackful boom 42")
	assert.Contains(t, out, "Stack:")
	assert.Contains(t, out, "[0]")
}

func TestPrintErrorWrappedMessages(t *testing.T) {
	out := captureStderr(t, func() {
		err := Newf("origin")
		err = Wrapf(err, "context")
		printError(err)
	})
	assert.Contains(t, out, "origin")
	assert.Contains(t, out, "context")
}

func TestPrintErrorPlain(t *testing.T) {
	out := captureStderr(t, func() {
		printError(errors.New("plain boom"))
	})
	// A non-stackful error takes the simple branch.
	assert.Contains(t, out, "Error: plain boom")
	assert.NotContains(t, out, "Stack:", "plain errors have no stack section")
}

// Fatal and Fatalf call os.Exit, so they can't run in the test process without
// killing it. The standard Go approach is to re-exec the test binary in a
// subprocess that runs the fatal path, then assert on its exit code and stderr
// from the parent. The environment variable selects which fatal case the child
// runs; the parent never sets it, so the switch below is a no-op in normal runs.
func TestMain(m *testing.M) {
	switch os.Getenv("EX_FATAL_CASE") {
	case "nil":
		Fatal(nil)
	case "single":
		Fatal(New("single fatal boom"))
	case "joined":
		Fatal(Join(New("first fatal"), New("second fatal")))
	case "fatalf":
		Fatalf("formatted fatal %d", 7)
	default:
		os.Exit(m.Run())
	}
}

// runFatalCase re-execs this test binary running only the requested fatal case
// and returns its combined stderr and the process exit error (non-nil on a
// non-zero exit, which every Fatal path produces).
func runFatalCase(t *testing.T, name string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestMain")
	cmd.Env = append(os.Environ(), "EX_FATAL_CASE="+name)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee, "Fatal should cause a non-zero exit")
	return ee.ExitCode()
}

func TestFatalNil(t *testing.T) {
	out, err := runFatalCase(t, "nil")
	assert.Equal(t, 1, exitCode(t, err))
	assert.Contains(t, out, "Fatal error: unknown")
}

func TestFatalSingle(t *testing.T) {
	out, err := runFatalCase(t, "single")
	assert.Equal(t, 1, exitCode(t, err))
	assert.Contains(t, out, "single fatal boom")
}

func TestFatalJoined(t *testing.T) {
	out, err := runFatalCase(t, "joined")
	assert.Equal(t, 1, exitCode(t, err))
	// Joined errors are unwrapped and printed one at a time.
	assert.Contains(t, out, "--- error 0 ---")
	assert.Contains(t, out, "first fatal")
	assert.Contains(t, out, "--- error 1 ---")
	assert.Contains(t, out, "second fatal")
}

func TestFatalf(t *testing.T) {
	out, err := runFatalCase(t, "fatalf")
	assert.Equal(t, 1, exitCode(t, err))
	assert.Contains(t, out, "formatted fatal 7")
	assert.Contains(t, out, "Stack:",
		"Fatalf builds a stackful error, so a stack section should be printed")
}
