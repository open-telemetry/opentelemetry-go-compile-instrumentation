// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
)

func BeforeConnect(ctx hook.HookContext, dsn string) {
	println("BeforeConnect")
}

func AfterConnect(ctx hook.HookContext, r1 error) {}
