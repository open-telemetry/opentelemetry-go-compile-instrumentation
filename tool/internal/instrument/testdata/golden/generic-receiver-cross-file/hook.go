// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func GenericMethodBefore(ctx hook.HookContext, recv interface{}, p1 interface{}) {}

func GenericMethodAfter(ctx hook.HookContext, r1 interface{}) {}
