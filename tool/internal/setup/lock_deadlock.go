// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// A parent otelc invocation holds its locks until its go command ends, so an
// otelc invocation started under that command, for example from a test, can
// wait for a lock that an unrelated invocation holds while that invocation, or
// one started under it, waits for a lock of this parent. Neither can then go
// on. To find such a cycle, an invocation that waits while it or a parent
// holds locks writes a wait file next to the lock it waits for. The wait file
// of lock L is L.wait.<random>, and it records when the wait started and the
// locks that the invocation and its parents hold. The invocation keeps the
// wait file locked while it waits, so a wait file that nobody holds was left
// by a process that ended and is ignored.

// waitFileInfix separates the lock path from the random part of the name of
// a wait file.
const waitFileInfix = ".wait."

// maxWaitFileSize is how much of a wait file a reader reads.
const maxWaitFileSize = 1 << 16

// deadlockCheckTicks is how many lock attempts a waiting invocation makes
// between two searches for a cycle.
const deadlockCheckTicks = 10

// waitRecord describes an invocation that waits for a lock.
type waitRecord struct {
	// file is the path of the wait file. It is empty when the invocation
	// could not write one.
	file string
	// lock is the lock the invocation waits for.
	lock string
	// since is when the wait started, in nanoseconds since the Unix epoch.
	since int64
	// holds are the locks that the invocation and its parents hold.
	holds []string
}

// registerWait writes the wait file of an invocation that waits for lock and
// holds the locks in holds. It returns the record of the wait and a function
// that removes the file.
func registerWait(ctx context.Context, lock string, holds []string) (waitRecord, func()) {
	self := waitRecord{lock: lock, since: time.Now().UnixNano(), holds: holds}
	content := strconv.FormatInt(self.since, 10) + "\n" + strings.Join(holds, "\n") + "\n"
	// A reader can lock the file before this invocation does, and then
	// removes it as left over. A new name avoids that file.
	const attempts = 3
	for range attempts {
		path := lock + waitFileInfix + rand.Text()
		flk, acquired, _, err := tryAcquire(path)
		if err != nil || !acquired {
			continue
		}
		release := releaseFunc(ctx, flk)
		if err = writeWaitFile(path, content); err != nil {
			release()
			continue
		}
		self.file = path
		return self, release
	}
	util.LoggerFromContext(ctx).DebugContext(ctx,
		"failed to write a wait file; other invocations cannot see this wait", "lock", lock)
	return self, func() {}
}

// writeWaitFile writes content to the wait file at path from offset 1. On
// Windows, the lock covers byte 0 and blocks reads and writes of that byte.
func writeWaitFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return ex.Wrapf(err, "opening wait file %s", path)
	}
	_, err = f.WriteAt([]byte(content), 1)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return ex.Wrapf(err, "writing wait file %s", path)
	}
	return nil
}

// waitFilePaths returns the paths of the wait files of lock.
func waitFilePaths(lock string) []string {
	dir := filepath.Dir(lock)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	prefix := filepath.Base(lock) + waitFileInfix
	var paths []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	return paths
}

// waitsFor returns the records of the invocations that wait for lock.
func waitsFor(ctx context.Context, lock string) []waitRecord {
	var records []waitRecord
	for _, path := range waitFilePaths(lock) {
		if record, ok := readWaitFile(ctx, path); ok {
			record.lock = lock
			records = append(records, record)
		}
	}
	return records
}

// removeLeftoverWaits removes the wait files of lock that no process holds. A
// process that ends while it waits, for example when it is killed, leaves its
// wait file behind.
func removeLeftoverWaits(ctx context.Context, lock string) {
	for _, path := range waitFilePaths(lock) {
		waitFileHeld(ctx, path)
	}
}

// waitFileHeld reports whether a process holds the wait file at path. It
// removes the file when no process holds it.
func waitFileHeld(ctx context.Context, path string) bool {
	flk, acquired, _, err := tryAcquire(path)
	if err != nil {
		return false
	}
	if acquired {
		releaseFunc(ctx, flk)()
		return false
	}
	return true
}

// readWaitFile reads the wait file at path. It reports false when no process
// holds the file, and then removes it, or when the file is not complete yet.
func readWaitFile(ctx context.Context, path string) (waitRecord, bool) {
	if !waitFileHeld(ctx, path) {
		return waitRecord{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return waitRecord{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.NewSectionReader(f, 1, maxWaitFileSize))
	if err != nil {
		return waitRecord{}, false
	}
	// Every line ends with a newline, so the last element is empty, or a
	// line the waiter has not finished writing.
	lines := strings.Split(string(data), "\n")
	lines = lines[:len(lines)-1]
	if len(lines) == 0 {
		return waitRecord{}, false
	}
	since, err := strconv.ParseInt(lines[0], 10, 64)
	if err != nil {
		return waitRecord{}, false
	}
	holds := slices.DeleteFunc(lines[1:], func(line string) bool { return line == "" })
	return waitRecord{file: path, since: since, holds: holds}, true
}

// findDeadlock searches for an invocation that holds self.lock and waits,
// directly or through other invocations, for a lock in self.holds. It returns
// the waits on that cycle, starting from the one next to self, or nil when
// there is none. An invocation waits for its parents to end, so an invocation
// whose wait file lists a lock waits for the holder of that lock.
func findDeadlock(ctx context.Context, self waitRecord) []waitRecord {
	type step struct {
		record waitRecord
		prev   *step
	}
	// reached maps each lock found so far to the wait through which it was
	// found, or to nil for the locks of self.
	reached := make(map[string]*step, len(self.holds))
	queue := slices.Clone(self.holds)
	for _, lock := range self.holds {
		reached[lock] = nil
	}
	for len(queue) > 0 {
		lock := queue[0]
		queue = queue[1:]
		for _, record := range waitsFor(ctx, lock) {
			if record.file == self.file {
				continue
			}
			s := &step{record: record, prev: reached[lock]}
			if slices.Contains(record.holds, self.lock) {
				var cycle []waitRecord
				for ; s != nil; s = s.prev {
					cycle = append(cycle, s.record)
				}
				return cycle
			}
			for _, held := range record.holds {
				if _, ok := reached[held]; !ok {
					reached[held] = s
					queue = append(queue, held)
				}
			}
		}
	}
	return nil
}

// deadlockError returns an error when waiting for self.lock can never end and
// self is the wait on the cycle that started last. The other waits on the
// cycle go on, and end once the invocation of self fails.
func deadlockError(ctx context.Context, self waitRecord) error {
	cycle := findDeadlock(ctx, self)
	if cycle == nil {
		return nil
	}
	for _, record := range cycle {
		if record.since > self.since || (record.since == self.since && record.file > self.file) {
			return nil
		}
	}
	return ex.Newf("waiting for build lock %s would deadlock: the otelc invocation that holds it waits, "+
		"directly or through other otelc invocations, for lock %s, which this invocation or its parent holds",
		self.lock, cycle[len(cycle)-1].lock)
}
