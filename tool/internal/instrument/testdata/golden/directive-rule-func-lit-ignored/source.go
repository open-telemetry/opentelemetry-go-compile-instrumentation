// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

func run(f func()) { f() }

// None of these directives annotates a function, so the rule injects nothing,
// including its imports, and logs a warning for each.
func main() {
	// The directive annotates the call to run, not its argument.
	//otelc:span
	run(func() {})

	// A trailing directive annotates nothing.
	f := func() {} //otelc:span
	f()

	//otelc:span
	value := 1
	println(value)
}
