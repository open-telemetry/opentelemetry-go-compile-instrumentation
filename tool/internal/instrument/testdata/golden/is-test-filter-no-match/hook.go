// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

// BeforeProcessRequest would be injected before ProcessRequest in a test build.
// The is_test: true filter scopes the rule to test builds; this fixture is a
// normal build (source.go, no _test.go file), so the rule is gated out and the
// hook is never injected. It exists only so the rule's inject_hooks path
// resolves to real sources.
func BeforeProcessRequest(ctx inst.HookContext, req string) {
	println("BeforeProcessRequest:", req)
}

// AfterProcessRequest would be injected after ProcessRequest in a test build.
func AfterProcessRequest(ctx inst.HookContext, err error) {}
