// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

// BeforeProcessRequest is the hook for the has_package: otherpkg rule. Because
// this fixture's source declares package main (not otherpkg), the declared
// package clause does not match and the rule is never applied here.
func BeforeProcessRequest(ctx inst.HookContext, req string) {
	println("BeforeProcessRequest:", req)
}

// AfterProcessRequest is the After hook counterpart.
func AfterProcessRequest(ctx inst.HookContext, err error) {}
