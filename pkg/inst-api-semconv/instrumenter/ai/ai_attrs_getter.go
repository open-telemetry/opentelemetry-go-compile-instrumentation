// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ai

type CommonAttrsGetter[REQUEST any, RESPONSE any] interface {
	GetAIOperationName(request REQUEST) string
	GetAISystem(request REQUEST) string
}

type LLMAttrsGetter[REQUEST any, RESPONSE any] interface {
	GetAIRequestModel(request REQUEST) string
	GetAIRequestEncodingFormats(request REQUEST) []string
	GetAIRequestFrequencyPenalty(request REQUEST) float64
	GetAIRequestPresencePenalty(request REQUEST) float64
	GetAIResponseFinishReasons(request REQUEST, response RESPONSE) []string
	GetAIResponseModel(request REQUEST, response RESPONSE) string
	GetAIRequestMaxTokens(request REQUEST) int64
	GetAIUsageInputTokens(request REQUEST) int64
	GetAIUsageOutputTokens(request REQUEST, response RESPONSE) int64
	GetAIRequestStopSequences(request REQUEST) []string
	GetAIRequestTemperature(request REQUEST) float64
	GetAIRequestTopK(request REQUEST) float64
	GetAIRequestTopP(request REQUEST) float64
	GetAIResponseID(request REQUEST, response RESPONSE) string
	GetAIServerAddress(request REQUEST) string
	GetAIRequestSeed(request REQUEST) int64
}
