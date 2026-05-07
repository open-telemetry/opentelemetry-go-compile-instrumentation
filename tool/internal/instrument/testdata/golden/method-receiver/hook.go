// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

func H3Before(ctx inst.HookContext, recv interface{}, p1 string, p2 int) {}

func H3After(ctx inst.HookContext, r1 float32, r2 error) {}

func H11Before(ctx inst.HookContext, recv interface{}) {}
