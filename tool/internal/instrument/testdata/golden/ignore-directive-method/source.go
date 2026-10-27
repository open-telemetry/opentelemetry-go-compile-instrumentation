// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

type T struct{}

//otelc:ignore
func (t *T) IgnoredMethod(p1 string) {}

func (t *T) InstrumentedMethod(p1 string) {}

func main() {}
