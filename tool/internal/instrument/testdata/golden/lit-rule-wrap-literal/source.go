// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "io"

func track(r *io.LimitedReader) *io.LimitedReader { return r }

func copyOf(r io.LimitedReader) io.LimitedReader { return r }

// A package-level literal is wrapped too.
var global = &io.LimitedReader{N: 1}

// &T{...} takes the pointer template.
func Pointer(r io.Reader) *io.LimitedReader {
	return &io.LimitedReader{R: r, N: 10}
}

// T{...} takes the value template.
func Value() io.LimitedReader {
	return io.LimitedReader{N: 20}
}

// &(T{...}) is a pointer too; wrapping the inner literal would not compile.
func ParenPointer() *io.LimitedReader {
	return &(io.LimitedReader{N: 30})
}

// set_fields skips a positional literal, but the literal is still wrapped.
func Positional() io.LimitedReader {
	return io.LimitedReader{nil, 40}
}

// The inner literal is wrapped before the outer one.
func Nested() *io.LimitedReader {
	return &io.LimitedReader{R: &io.LimitedReader{N: 5}, N: 50}
}

func main() {
	_ = global
	_ = Pointer(nil)
	_ = Value()
	_ = ParenPointer()
	_ = Positional()
	_ = Nested()
}
