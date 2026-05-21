// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package wrapper provides helper functions used by golden tests to validate
// that call rules can correctly reference imported helper packages via the
// imports field. This package is intentionally outside testdata/ so that the
// Go build system can resolve it during test compilation.
package wrapper

// Sizeof wraps a uintptr value, demonstrating how an imported helper function
// can be used in a call rule's replace template (e.g. "wrapper.Sizeof({{ . }})").
func Sizeof(size uintptr) uintptr {
	return size
}
