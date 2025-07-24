// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

func Assert(condition bool, message string) {
	if !condition {
		ex.Fatalf("Assertion failed: %s", message)
	}
}

func ShouldNotReachHere() {
	ex.Fatalf("should not reach here")
}

func Unimplemented() {
	ex.Fatalf("Unimplemented yet")
}
