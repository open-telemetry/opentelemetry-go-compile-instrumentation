// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package streaming

import (
	"go.opentelemetry.io/otel/attribute"
)

// Attribute keys used by StreamingReader. Kept local to this package rather
// than imported from a per-version semconv package: this module is shared by
// v1/v2/v3, and importing any single version's semconv package back would
// create a module dependency cycle with that version's go.mod, which in turn
// requires this package.
const (
	genAIResponseModelKey            = attribute.Key("gen_ai.response.model")
	genAIResponseIDKey               = attribute.Key("gen_ai.response.id")
	genAIResponseFinishReasonsKey    = attribute.Key("gen_ai.response.finish_reasons")
	genAIUsageInputTokensKey         = attribute.Key("gen_ai.usage.input_tokens")
	genAIUsageOutputTokensKey        = attribute.Key("gen_ai.usage.output_tokens")
	genAIUsageTotalTokensKey         = attribute.Key("gen_ai.usage.total_tokens")
	genAIResponseTimeToFirstTokenKey = attribute.Key("gen_ai.response.time_to_first_token")
)
