// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppOutput_WaitForReturnsWhenAlreadyWritten(t *testing.T) {
	var out AppOutput
	_, err := out.Write([]byte(`{"msg":"rpc completed"}` + "\n"))
	require.NoError(t, err)

	require.NoError(t, out.WaitFor(t.Context(), "rpc completed"))
}

func TestAppOutput_WaitForUnblocksOnLaterWrite(t *testing.T) {
	var out AppOutput

	done := make(chan error, 1)
	go func() { done <- out.WaitFor(t.Context(), "rpc completed") }()

	// Whichever order these interleave, the wait must be satisfied: output
	// written before WaitFor registers still counts.
	_, err := out.Write([]byte(`{"msg":"rpc completed"}` + "\n"))
	require.NoError(t, err)

	require.NoError(t, <-done)
}

func TestAppOutput_WaitForFailsWhenContextDone(t *testing.T) {
	var out AppOutput
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := out.WaitFor(ctx, "never written")
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "never written")
}

func TestAppOutput_WaitForWakesOnlyMatchingWaiters(t *testing.T) {
	var out AppOutput

	matched := make(chan error, 1)
	go func() { matched <- out.WaitFor(t.Context(), "first") }()

	unmatchedCtx, cancelUnmatched := context.WithCancel(t.Context())
	unmatched := make(chan error, 1)
	go func() { unmatched <- out.WaitFor(unmatchedCtx, "second") }()

	_, err := out.Write([]byte("first line\n"))
	require.NoError(t, err)
	require.NoError(t, <-matched)

	// The unmatched waiter is still pending, so it ends only with its context.
	cancelUnmatched()
	require.ErrorIs(t, <-unmatched, context.Canceled)
}

func TestAppOutput_ConcurrentWritesAreRecorded(t *testing.T) {
	var out AppOutput

	// Writes report back through a channel: testify must only be called from
	// the goroutine running the test.
	errs := make(chan error, 10)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_, err := out.Write([]byte("x"))
			errs <- err
		})
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, 10, out.Len())
	require.Equal(t, "xxxxxxxxxx", out.String())
}
