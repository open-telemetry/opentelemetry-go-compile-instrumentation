// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package runtime stands in for the real "runtime" package in this golden
// test. otelc's real build adds SuppressHooks and UnsuppressHooks to the
// actual runtime package source; this harness compiles one file at a time
// against prebuilt stdlib archives, so it cannot do that. The test harness
// compiles this file in the real package's place instead.
package runtime

func SuppressHooks()   {}
func UnsuppressHooks() {}
