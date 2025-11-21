// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"reflect"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

func Assert(condition bool, message string) {
	if !condition {
		ex.Fatalf("Assertion failed: %s", message)
	}
}

func AssertType[T any](v any) T {
	value, ok := v.(T)
	if !ok {
		actualType := reflect.TypeOf(v).Name()
		var zero T
		expectType := reflect.TypeOf(zero).String()
		ex.Fatalf("Type assertion failed: %s, expected %s",
			actualType, expectType)
	}
	return value
}

func ShouldNotReachHere() {
	ex.Fatalf("Should not reach here!")
}

func Unimplemented(message string) {
	ex.Fatalf("Unimplemented: %s", message)
}
