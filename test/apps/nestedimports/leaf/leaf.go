// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package leaf is added to inner by a rule, and the application imports it
// too, so the outer build links its own instrumented archive of it. inner must
// be compiled against that same archive.
package leaf

import "fmt"

// Instrumented is flipped to true by the leaf rule. It stays false when the
// build compiled this package uninstrumented.
var Instrumented = false

// Note is called by the code the inner rule injected.
func Note() {
	fmt.Println("nestedimports: inner rule called into leaf")
}
