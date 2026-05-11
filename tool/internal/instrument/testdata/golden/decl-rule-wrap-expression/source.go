// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// wrapTransport simulates an OTel instrumentation wrapper in the test.
// In production rules this would be an imported package function.
func wrapTransport(t interface{}) interface{} {
	return t
}

var DefaultTransport interface{} = struct{ name string }{"default"}

func main() {}
