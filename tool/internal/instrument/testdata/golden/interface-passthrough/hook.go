// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func H15InterfaceBefore(ctx hook.HookContext, v interface{}) {}

func H15InterfaceAfter(ctx hook.HookContext, r any) {}
