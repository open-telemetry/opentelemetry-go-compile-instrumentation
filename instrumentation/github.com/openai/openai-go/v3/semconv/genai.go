// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"go.opentelemetry.io/otel/attribute"
)

// gen_ai.system is deprecated upstream, so go.opentelemetry.io/otel/semconv/v1.37.0
// omits it. Everything else is imported from upstream or from pkg/otelcconv.
const (
	GenAISystemKey = attribute.Key("gen_ai.system")
)

func GenAISystem(val string) attribute.KeyValue {
	return GenAISystemKey.String(val)
}
