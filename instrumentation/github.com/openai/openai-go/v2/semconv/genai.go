// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"go.opentelemetry.io/otel/attribute"
)

// Only the attributes that go.opentelemetry.io/otel/semconv/v1.37.0 does not
// provide are declared here. gen_ai.system is deprecated upstream, so the
// upstream package omits it; the rest are not defined in v1.37.0.
// Everything else is imported from upstream directly.
const (
	GenAISystemKey                   = attribute.Key("gen_ai.system")
	GenAIUsageTotalTokensKey         = attribute.Key("gen_ai.usage.total_tokens")
	GenAIRequestIsStreamKey          = attribute.Key("gen_ai.request.is_stream")
	GenAIResponseTimeToFirstTokenKey = attribute.Key("gen_ai.response.time_to_first_token")

	// GenAIUsageCacheReadInputTokensKey reports prompt tokens served from
	// OpenAI's automatic prompt cache. Unlike Anthropic, OpenAI already counts
	// these inside prompt_tokens, so this is a breakdown of the input count and
	// must not be added to it.
	GenAIUsageCacheReadInputTokensKey = attribute.Key("gen_ai.usage.cache_read.input_tokens")
)

func GenAISystem(val string) attribute.KeyValue {
	return GenAISystemKey.String(val)
}

func GenAIUsageTotalTokens(val int64) attribute.KeyValue {
	return GenAIUsageTotalTokensKey.Int64(val)
}

func GenAIUsageCacheReadInputTokens(val int64) attribute.KeyValue {
	return GenAIUsageCacheReadInputTokensKey.Int64(val)
}

func GenAIRequestIsStream(val bool) attribute.KeyValue {
	return GenAIRequestIsStreamKey.Bool(val)
}

func GenAIResponseTimeToFirstToken(microseconds int64) attribute.KeyValue {
	return GenAIResponseTimeToFirstTokenKey.Int64(microseconds)
}
