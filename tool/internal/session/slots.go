// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/gofrs/flock"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// slotsDir is the session subdirectory that holds the load slot files.
const slotsDir = "slots"

// slotHeldKey marks a context whose call chain already holds a load slot.
type slotHeldKey struct{}

// maxParallelLoads returns the number of load slots: the positive integer in
// util.EnvOtelcMaxParallelLoads, 0 for no bound, else runtime.NumCPU().
func maxParallelLoads() int {
	if v := os.Getenv(util.EnvOtelcMaxParallelLoads); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return runtime.NumCPU()
}

// WithLoadSlot runs fn while holding one of the session's load slots, so at
// most a bounded number of heavy package loads run at once across every otelc
// process of the session. The wait has no deadline; ctx cancels it. It runs
// fn directly when the process inherits no session, when the bound is 0, or
// when the call chain already holds a slot.
//
// fn must never wait on an otelc process that takes a slot itself, or a slot
// holder could wait on its own descendant. Nested toolexec processes and
// compile-time resolution therefore never take one.
func WithLoadSlot(ctx context.Context, fn func(context.Context) error) error {
	if held, _ := ctx.Value(slotHeldKey{}).(bool); held || !util.InheritedSession() {
		return fn(ctx)
	}
	n := maxParallelLoads()
	if n == 0 {
		return fn(ctx)
	}
	release, wait, err := acquireSlot(ctx, filepath.Join(util.GetSessionDir(), slotsDir), n)
	if err != nil {
		return err
	}
	defer release()
	start := time.Now()
	err = fn(context.WithValue(ctx, slotHeldKey{}, true))
	if os.Getenv(util.EnvOtelcStats) != "" {
		util.LoggerFromContext(ctx).InfoContext(ctx, "setup load stats",
			"slot_wait", wait, "duration", time.Since(start))
	}
	return err
}

// acquireSlot takes the first free of n slot files in dir and reports how
// long it waited.
func acquireSlot(ctx context.Context, dir string, n int) (func(), time.Duration, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, 0, ex.Wrapf(err, "creating load slot directory %s", dir)
	}
	start := time.Now()
	// One handle per slot file for the whole wait, so a busy poll costs a
	// flock call per slot instead of an open and a close. Starting at a
	// random slot spreads concurrent waiters over the slots.
	locks := make([]*flock.Flock, n)
	for i := range n {
		locks[i] = flock.New(filepath.Join(dir, fmt.Sprintf("slot-%d.lock", i)))
	}
	closeExcept := func(keep int) {
		for i, lock := range locks {
			if i != keep {
				_ = lock.Close()
			}
		}
	}
	first := rand.IntN(n)
	logged := false
	ticker := time.NewTicker(lockRetryInterval)
	defer ticker.Stop()
	for {
		for k := range n {
			i := (first + k) % n
			ok, err := tryLock(locks[i])
			if err != nil {
				closeExcept(-1)
				return nil, 0, err
			}
			if ok {
				closeExcept(i)
				return releaseLock(locks[i]), time.Since(start), nil
			}
		}
		if !logged {
			logged = true
			util.LoggerFromContext(ctx).InfoContext(ctx,
				"waiting for a free package load slot", "slots", n, "dir", dir)
		}
		select {
		case <-ctx.Done():
			closeExcept(-1)
			return nil, 0, ex.Wrapf(ctx.Err(), "waiting for a package load slot in %s", dir)
		case <-ticker.C:
		}
	}
}
