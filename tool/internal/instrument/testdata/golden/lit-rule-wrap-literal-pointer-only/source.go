// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "io"

func track(r *io.LimitedReader) *io.LimitedReader { return r }

// &T{...} is wrapped.
func Pointer() *io.LimitedReader {
	return &io.LimitedReader{N: 10}
}

// T{...} has no value template, so it is left alone.
func Value() io.LimitedReader {
	return io.LimitedReader{N: 20}
}

func main() {
	_ = Pointer()
	_ = Value()
}
