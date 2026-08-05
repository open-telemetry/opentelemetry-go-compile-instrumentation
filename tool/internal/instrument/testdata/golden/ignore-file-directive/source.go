// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//otelc:ignore

package main

func SkippedFunc(p1 string) {}

//otelc:instrument
func ForcedFunc(p1 string) {}

func main() {}
