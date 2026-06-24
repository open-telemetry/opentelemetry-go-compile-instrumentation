// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
)

func H3Before(ctx hook.HookContext, recv interface{}, p1 string, p2 int) {}

func H3After(ctx hook.HookContext, r1 float32, r2 error) {}

func H11Before(ctx hook.HookContext, recv interface{}) {}
