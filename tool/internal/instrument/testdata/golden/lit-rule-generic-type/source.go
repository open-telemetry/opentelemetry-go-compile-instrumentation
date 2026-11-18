// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/lit-rule-generic-type/helpers/generic"

func main() {
	_ = generic.Box[int]{Value: 1}
	_ = &generic.Box[string]{Value: "a"}
	_ = generic.Pair[string, int]{Key: "k", Val: 2}
}
