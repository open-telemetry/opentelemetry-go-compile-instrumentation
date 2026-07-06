// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

// BeforeExternalHelper is injected before ExternalHelper. The all-of
// [is_test: true, has_package: main_test] composition selects this external
// test package via its declared package clause, not its import path.
func BeforeExternalHelper(ctx inst.HookContext) {
	println("BeforeExternalHelper")
}

// AfterExternalHelper is injected after ExternalHelper.
func AfterExternalHelper(ctx inst.HookContext, err error) {}
