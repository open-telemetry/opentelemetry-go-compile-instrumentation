// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ex

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
