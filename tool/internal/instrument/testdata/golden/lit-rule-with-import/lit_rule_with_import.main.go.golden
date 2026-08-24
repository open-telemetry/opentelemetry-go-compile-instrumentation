// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// The literal is reached through an import alias, and the injected value needs
// an import the file does not already have.
import stdio "io"

func NewReader() *stdio.LimitedReader {
	return &stdio.LimitedReader{N: 10}
}

func main() {
	_ = NewReader()
}
