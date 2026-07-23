// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package runtime

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRaiseSignalToSelfDeliversSignal(t *testing.T) {
	// Use SIGUSR1: it's a benign signal the shutdown handler doesn't listen for,
	// so re-raising it can't wake that handler and race sibling tests. Catch it
	// here so the default disposition doesn't terminate the test binary.
	// SIGUSR1 is Unix-only, hence the build tag.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	t.Cleanup(func() { signal.Stop(ch) })

	raiseSignalToSelf(syscall.SIGUSR1)

	select {
	case got := <-ch:
		assert.Equal(t, syscall.SIGUSR1, got)
	case <-time.After(2 * time.Second):
		t.Fatal("raiseSignalToSelf did not deliver the signal")
	}
}
