// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

// BeforeProcessRequest is invoked before ProcessRequest in production builds.
// The is_test: false filter ensures this hook is never injected into test binaries.
func BeforeProcessRequest(ctx inst.HookContext, req string) {
	println("BeforeProcessRequest:", req)
}

// AfterProcessRequest is invoked after ProcessRequest in production builds.
func AfterProcessRequest(ctx inst.HookContext, err error) {}
