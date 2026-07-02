// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

// BeforeProcessRequest is injected before ProcessRequest in this test build.
// The is_test: true filter scopes the rule to test builds (here, a _test.go
// source set).
func BeforeProcessRequest(ctx inst.HookContext, req string) {
	println("BeforeProcessRequest:", req)
}

// AfterProcessRequest is injected after ProcessRequest in this test build.
func AfterProcessRequest(ctx inst.HookContext, err error) {}
