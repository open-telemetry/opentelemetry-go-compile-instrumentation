// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build ignore

package sql

// OtelTrackRows synchronizes callbacks with iteration and closure, including
// cancellation that closes Rows before the query after-hook runs.
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

// otelEndRows runs after close releases closemu. Detach callbacks under the write
// lock for exactly-once emission; invoke exporters outside the lock.
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
