// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package helloworld

// Example demonstrates how to use the instrumenter.
func Example() {
	MyHook()
	// Output:
	// [MyHook] start to instrument hello world!
	// [MyHook] hello world is instrumented!
}
