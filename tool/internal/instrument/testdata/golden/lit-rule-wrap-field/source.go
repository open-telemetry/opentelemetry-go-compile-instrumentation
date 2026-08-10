// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "io"

func counting(r io.Reader) io.Reader { return r }

// Both fields are set, so both are wrapped.
func Wrapped(r io.Reader) *io.LimitedReader {
	return &io.LimitedReader{R: r, N: 10}
}

// R is absent and takes its value; N is wrap-only and stays absent.
func Absent() *io.LimitedReader {
	return &io.LimitedReader{}
}

func main() {
	_ = Wrapped(nil)
	_ = Absent()
}
