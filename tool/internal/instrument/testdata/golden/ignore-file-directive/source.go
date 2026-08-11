// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//otelc:ignore

package main

func SkippedFunc(p1 string) {}

//otelc:instrument
func ForcedFunc(p1 string) {}

// BothFunc carries both directives; //otelc:ignore must win over
// //otelc:instrument, so it is not instrumented.
//otelc:instrument
//otelc:ignore
func BothFunc(p1 string) {}

// T is targeted by a non-func (struct) rule. File-level //otelc:ignore skips
// non-func rules unconditionally (they are not overridable), so no field is
// added.
type T struct{}

func main() {}
