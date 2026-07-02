// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"go.opentelemetry.io/otel/attribute"
)

const (
	GenAISystemKey                    = attribute.Key("gen_ai.system")
	GenAIOperationNameKey             = attribute.Key("gen_ai.operation.name")
	GenAIRequestModelKey              = attribute.Key("gen_ai.request.model")
	GenAIResponseModelKey             = attribute.Key("gen_ai.response.model")
	GenAIResponseIDKey                = attribute.Key("gen_ai.response.id")
	GenAIResponseFinishReasonsKey     = attribute.Key("gen_ai.response.finish_reasons")
	GenAIUsageInputTokensKey          = attribute.Key("gen_ai.usage.input_tokens")
	GenAIUsageOutputTokensKey         = attribute.Key("gen_ai.usage.output_tokens")
	GenAIUsageTotalTokensKey          = attribute.Key("gen_ai.usage.total_tokens")
	GenAIProviderNameKey              = attribute.Key("gen_ai.provider.name")
	GenAIRequestMaxTokensKey          = attribute.Key("gen_ai.request.max_tokens")
	GenAIRequestTemperatureKey        = attribute.Key("gen_ai.request.temperature")
	GenAIRequestTopPKey               = attribute.Key("gen_ai.request.top_p")
	GenAIRequestFrequencyPenaltyKey   = attribute.Key("gen_ai.request.frequency_penalty")
	GenAIRequestPresencePenaltyKey    = attribute.Key("gen_ai.request.presence_penalty")
	GenAIRequestIsStreamKey           = attribute.Key("gen_ai.request.is_stream")
	GenAIResponseTimeToFirstTokenKey  = attribute.Key("gen_ai.response.time_to_first_token")
)

func GenAISystem(val string) attribute.KeyValue {
	return GenAISystemKey.String(val)
}

func GenAIOperationName(val string) attribute.KeyValue {
	return GenAIOperationNameKey.String(val)
}

func GenAIRequestModel(val string) attribute.KeyValue {
	return GenAIRequestModelKey.String(val)
}

func GenAIResponseModel(val string) attribute.KeyValue {
	return GenAIResponseModelKey.String(val)
}

func GenAIResponseID(val string) attribute.KeyValue {
	return GenAIResponseIDKey.String(val)
}

func GenAIResponseFinishReasons(val []string) attribute.KeyValue {
	return GenAIResponseFinishReasonsKey.StringSlice(val)
}

func GenAIUsageInputTokens(val int64) attribute.KeyValue {
	return GenAIUsageInputTokensKey.Int64(val)
}

func GenAIUsageOutputTokens(val int64) attribute.KeyValue {
	return GenAIUsageOutputTokensKey.Int64(val)
}

func GenAIUsageTotalTokens(val int64) attribute.KeyValue {
	return GenAIUsageTotalTokensKey.Int64(val)
}

func GenAIProviderName(val string) attribute.KeyValue {
	return GenAIProviderNameKey.String(val)
}

func GenAIRequestMaxTokens(val int64) attribute.KeyValue {
	return GenAIRequestMaxTokensKey.Int64(val)
}

func GenAIRequestTemperature(val float64) attribute.KeyValue {
	return GenAIRequestTemperatureKey.Float64(val)
}

func GenAIRequestTopP(val float64) attribute.KeyValue {
	return GenAIRequestTopPKey.Float64(val)
}

func GenAIRequestFrequencyPenalty(val float64) attribute.KeyValue {
	return GenAIRequestFrequencyPenaltyKey.Float64(val)
}

func GenAIRequestPresencePenalty(val float64) attribute.KeyValue {
	return GenAIRequestPresencePenaltyKey.Float64(val)
}

func GenAIRequestIsStream(val bool) attribute.KeyValue {
	return GenAIRequestIsStreamKey.Bool(val)
}

func GenAIResponseTimeToFirstToken(microseconds int64) attribute.KeyValue {
	return GenAIResponseTimeToFirstTokenKey.Int64(microseconds)
}

