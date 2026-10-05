// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "io"

// Go rejects a literal mixing keyed and positional elements, so this one is
// skipped and left exactly as written.
func Positional(r io.Reader) io.LimitedReader {
	return io.LimitedReader{r, 2}
}

// The keyed form in the same file is still instrumented.
func Keyed(r io.Reader) io.LimitedReader {
	return io.LimitedReader{R: r}
}

func main() {
	_ = Positional(nil)
	_ = Keyed(nil)
}
