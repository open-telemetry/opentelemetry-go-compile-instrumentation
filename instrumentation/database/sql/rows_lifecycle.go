// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build ignore

package sql

// OtelTrackRows installs callbacks under the same lock used by iteration and
// closure. Cancellation can close Rows before QueryContext's after-hook runs;
// in that case the callback is invoked immediately instead of being lost.
func (rs *Rows) OtelTrackRows(onNext func(), onClose func(error)) {
	rs.closemu.Lock()
	if rs.closed {
		err := rs.otelRowsErrorLocked()
		rs.closemu.Unlock()
		onClose(err)
		return
	}
	rs.otelRowsNext = onNext
	rs.otelRowsClose = onClose
	rs.closemu.Unlock()
}

// otelEndRows runs after close has released closemu. Clear the callbacks while
// holding the write lock, so repeated/concurrent closes emit once and no row
// increment is still in flight. Exporters run only after the lock is released.
func (rs *Rows) otelEndRows() {
	rs.closemu.Lock()
	onClose := rs.otelRowsClose
	rs.otelRowsNext = nil
	rs.otelRowsClose = nil
	err := rs.otelRowsErrorLocked()
	rs.closemu.Unlock()
	if onClose != nil {
		onClose(err)
	}
}

// Do not call Err here: it reads hitEOF, which belongs to the iteration
// goroutine and may still be changing when cancellation closes Rows.
func (rs *Rows) otelRowsErrorLocked() error {
	if err := rs.lasterrOrErrLocked(nil); err != nil {
		return err
	}
	if rs.lasterr == nil {
		if err := rs.contextDone.Load(); err != nil {
			return *err
		}
	}
	return nil
}
