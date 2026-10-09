// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"bytes"
	"context"
	"strings"
	"sync"

	"go.opentelemetry.io/otelc/tool/ex"
)

// AppOutput collects a running application's combined stdout and stderr and
// lets a test block until the application logs a particular string.
//
// Waiting is event driven rather than polled: each write wakes exactly the
// waiters it satisfies. Tests therefore synchronise on the application having
// reached a known point instead of sleeping and hoping it got there.
type AppOutput struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	waiters []outputWaiter
}

// outputWaiter is one pending WaitFor call. ready is closed once substr has
// been written.
type outputWaiter struct {
	substr string
	ready  chan struct{}
}

// Write implements io.Writer so an AppOutput can be used as a command's Stdout
// and Stderr. It records the bytes and releases any waiter they satisfy.
func (o *AppOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	n, err := o.buf.Write(p)

	written := o.buf.String()
	pending := o.waiters[:0]
	for _, w := range o.waiters {
		if strings.Contains(written, w.substr) {
			close(w.ready)
			continue
		}
		pending = append(pending, w)
	}
	o.waiters = pending

	return n, err
}

// WaitFor blocks until substr has been written, returning an error if ctx is
// done first. Output already written counts, so a caller cannot miss a line by
// arriving late.
func (o *AppOutput) WaitFor(ctx context.Context, substr string) error {
	o.mu.Lock()
	if strings.Contains(o.buf.String(), substr) {
		o.mu.Unlock()
		return nil
	}
	ready := make(chan struct{})
	o.waiters = append(o.waiters, outputWaiter{substr: substr, ready: ready})
	o.mu.Unlock()

	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ex.Wrapf(ctx.Err(), "waiting for app output %q", substr)
	}
}

// String returns everything written so far.
func (o *AppOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// Len returns how many bytes have been written so far.
func (o *AppOutput) Len() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Len()
}
