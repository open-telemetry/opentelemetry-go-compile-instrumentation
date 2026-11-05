// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package late reaches the build twice: the application imports it directly,
// and the dep rule adds it to dep. The outer build and the nested one must
// compile the same instrumented archive of it. The integration test pads this
// package with generated functions before building, so instrumenting it
// outlasts the nested resolution that dep's compile starts.
package late

import "fmt"

// Instrumented is flipped to true by the late rule. It stays false when the
// build compiled this package uninstrumented.
var Instrumented = false

// Note is called by the code the dep rule injected. Its output proves the
// archive the nested build resolved linked into dep.
func Note() {
	fmt.Println("nestedimports: dep rule called into late")
}
