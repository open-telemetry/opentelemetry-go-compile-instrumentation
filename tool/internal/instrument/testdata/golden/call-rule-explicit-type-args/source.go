// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/call-rule-explicit-type-args/helpers/generic"

func trace[T any](v T) T { return v }

func name(int) string { return "n" }

func main() {
	// The type argument is inferred.
	_ = generic.Id(1)
	// The type argument is written out.
	_ = generic.Id[int](2)
	// Two type arguments are written out.
	_ = generic.Convert[int, string](3, name)
}
