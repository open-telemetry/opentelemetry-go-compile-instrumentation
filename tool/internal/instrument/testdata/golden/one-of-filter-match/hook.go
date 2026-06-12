// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

func BeforeOpen(ctx inst.HookContext, dsn string) {
	println("BeforeOpen")
}

func AfterOpen(ctx inst.HookContext, r1 error) {}
