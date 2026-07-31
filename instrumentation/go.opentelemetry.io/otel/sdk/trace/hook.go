// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace

import (
	"go.opentelemetry.io/otelc/pkg/hook"
)

func afterNewRecordingSpan(_ hook.HookContext, span any) {
	addSpanToGLS(span)
}

func afterNewNonRecordingSpan(_ hook.HookContext, span any) {
	addSpanToGLS(span)
}

func beforeRecordingSpanEnd(_ hook.HookContext, span any, _ ...any) {
	deleteSpanFromGLS(span)
}

func beforeNonRecordingSpanEnd(_ hook.HookContext, span any, _ ...any) {
	deleteSpanFromGLS(span)
}
