// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

func HBefore(ctx inst.HookContext, p1 string) {}
func HAfter(ctx inst.HookContext)              {}
