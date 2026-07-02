// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

// BeforeProcessRequest is injected before ProcessRequest. The is_test: false
// filter scopes the rule to non-test builds, so this hook is never injected
// into a test build.
func BeforeProcessRequest(ctx inst.HookContext, req string) {
	println("BeforeProcessRequest:", req)
}

// AfterProcessRequest is injected after ProcessRequest in non-test builds.
func AfterProcessRequest(ctx inst.HookContext, err error) {}
