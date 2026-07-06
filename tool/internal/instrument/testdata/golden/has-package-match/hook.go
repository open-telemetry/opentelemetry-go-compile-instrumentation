// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

// BeforeProcessRequest is injected before ProcessRequest. The has_package:
// main_test filter matches the external test file's declared package clause,
// proving has_package evaluates the AST name independently from the import path.
func BeforeProcessRequest(ctx inst.HookContext, req string) {
	println("BeforeProcessRequest:", req)
}

// AfterProcessRequest is injected after ProcessRequest.
func AfterProcessRequest(ctx inst.HookContext, err error) {}
