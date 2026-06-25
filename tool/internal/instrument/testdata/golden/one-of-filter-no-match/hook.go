// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
)

func BeforeOpen(ctx hook.HookContext, dsn string) {
	println("BeforeOpen")
}

func AfterOpen(ctx hook.HookContext, r1 error) {}
