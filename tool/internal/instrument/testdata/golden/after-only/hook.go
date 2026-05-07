// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

func H1After(ctx inst.HookContext, r1 float32, r2 error) {}

func H8After(ctx inst.HookContext, ret1 float32, ret2 error) {}
