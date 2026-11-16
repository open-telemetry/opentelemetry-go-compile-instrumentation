// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"bytes"
	"context"
	"log/slog"
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

func TestWithLoadSlotBoundsConcurrency(t *testing.T) {
	t.Setenv(util.EnvOtelcSessionDir, t.TempDir())
	t.Setenv(util.EnvOtelcMaxParallelLoads, "1")

	var inside, maxInside atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			err := WithLoadSlot(t.Context(), func(context.Context) error {
				n := inside.Add(1)
				for {
					m := maxInside.Load()
					if n <= m || maxInside.CompareAndSwap(m, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				inside.Add(-1)
				return nil
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	assert.Equal(t, int32(1), maxInside.Load())
}

func TestWithLoadSlotIsReentrant(t *testing.T) {
	t.Setenv(util.EnvOtelcSessionDir, t.TempDir())
	t.Setenv(util.EnvOtelcMaxParallelLoads, "1")

	ran := false
	err := WithLoadSlot(t.Context(), func(ctx context.Context) error {
		return WithLoadSlot(ctx, func(context.Context) error {
			ran = true
			return nil
		})
	})
	require.NoError(t, err)
	assert.True(t, ran)
}

func TestWithLoadSlotWithoutSession(t *testing.T) {
	t.Setenv(util.EnvOtelcSessionDir, "")
	t.Setenv(util.EnvOtelcMaxParallelLoads, "1")

	// Without an inherited session the slot is a no-op, so two holders
	// nest freely even with one slot.
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		_ = WithLoadSlot(t.Context(), func(context.Context) error {
			<-release
			return nil
		})
	}()
	go func() {
		_ = WithLoadSlot(t.Context(), func(context.Context) error { return nil })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a load without a session waited on another")
	}
	close(release)
}

func TestWithLoadSlotCanceled(t *testing.T) {
	t.Setenv(util.EnvOtelcSessionDir, t.TempDir())
	t.Setenv(util.EnvOtelcMaxParallelLoads, "1")

	hold := make(chan struct{})
	held := make(chan struct{})
	go func() {
		_ = WithLoadSlot(context.Background(), func(context.Context) error {
			close(held)
			<-hold
			return nil
		})
	}()
	<-held
	defer close(hold)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	err := WithLoadSlot(ctx, func(context.Context) error {
		t.Error("ran without a free slot")
		return nil
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestMaxParallelLoads(t *testing.T) {
	t.Setenv(util.EnvOtelcMaxParallelLoads, "3")
	assert.Equal(t, 3, maxParallelLoads())
	t.Setenv(util.EnvOtelcMaxParallelLoads, "0")
	assert.Equal(t, 0, maxParallelLoads())
	t.Setenv(util.EnvOtelcMaxParallelLoads, "nope")
	assert.Positive(t, maxParallelLoads())
}

func TestWithLoadSlotUnboundedWithZero(t *testing.T) {
	t.Setenv(util.EnvOtelcSessionDir, t.TempDir())
	t.Setenv(util.EnvOtelcMaxParallelLoads, "0")

	// With no bound, a load runs while another one still holds.
	holding := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = WithLoadSlot(t.Context(), func(context.Context) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	defer close(release)

	done := make(chan error, 1)
	go func() { done <- WithLoadSlot(t.Context(), func(context.Context) error { return nil }) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("a load waited although loads are unbounded")
	}
}

func TestWithLoadSlotLogsStats(t *testing.T) {
	t.Setenv(util.EnvOtelcSessionDir, t.TempDir())
	t.Setenv(util.EnvOtelcMaxParallelLoads, "1")
	t.Setenv(util.EnvOtelcStats, "1")
	var buf bytes.Buffer
	ctx := util.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(&buf, nil)))

	require.NoError(t, WithLoadSlot(ctx, func(context.Context) error { return nil }))
	assert.Contains(t, buf.String(), "setup load stats")
	assert.Contains(t, buf.String(), "slot_wait=")
}

func TestWithLoadSlotFailsWhenSessionIsNotADirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o644))
	t.Setenv(util.EnvOtelcSessionDir, blocker)
	t.Setenv(util.EnvOtelcMaxParallelLoads, "1")

	err := WithLoadSlot(t.Context(), func(context.Context) error {
		t.Error("ran without a slot")
		return nil
	})
	require.Error(t, err)
}
