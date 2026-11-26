// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"sync"
)

// lazyLogFile is the debug log file a toolexec process opens on demand. The
// go toolchain runs otelc as -toolexec once per build action, and most of
// those processes emit no record at the enabled level, so the file is only
// created when the first record actually reaches a handler. It is safe for
// concurrent use.
type lazyLogFile struct {
	mu      sync.Mutex
	path    string
	file    *os.File
	openErr error
}

func (lf *lazyLogFile) Write(p []byte) (int, error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	if lf.file == nil {
		if lf.openErr != nil {
			return 0, lf.openErr
		}
		file, err := os.OpenFile(lf.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			lf.openErr = err
			return 0, err
		}
		lf.file = file
	}
	return lf.file.Write(p)
}

// Close releases the log file when it was opened, or returns the first open
// error when opening failed. A toolexec run that logged nothing never created
// it, so Close is a no-op then.
func (lf *lazyLogFile) Close() error {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	if lf.file == nil {
		err := lf.openErr
		lf.openErr = nil
		return err
	}
	err := lf.file.Close()
	lf.file = nil
	return err
}
