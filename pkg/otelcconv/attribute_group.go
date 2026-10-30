// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Code generated from semantic convention specification. DO NOT EDIT.

package otelcconv

import "go.opentelemetry.io/otel/attribute"

// Namespace: aws
const (
	// AWSRegionKey is the attribute Key conforming to the "aws.region" semantic
	// conventions. It represents the AWS region the client used to serve the
	// request.
	//
	// Type: string
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: "us-east-1"
	AWSRegionKey = attribute.Key("aws.region")
)

// AWSRegion returns an attribute KeyValue conforming to the "aws.region"
// semantic conventions. It represents the AWS region the client used to serve
// the request.
func AWSRegion(val string) attribute.KeyValue {
	return AWSRegionKey.String(val)
}

// Namespace: gen_ai
const (
	// GenAIRequestIsStreamKey is the attribute Key conforming to the
	// "gen_ai.request.is_stream" semantic conventions. It represents the whether
	// the request was served as a streaming (server-sent events) response.
	//
	// Type: boolean
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples:
	GenAIRequestIsStreamKey = attribute.Key("gen_ai.request.is_stream")

	// GenAIResponseTimeToFirstTokenKey is the attribute Key conforming to the
	// "gen_ai.response.time_to_first_token" semantic conventions. It represents the
	// time from sending the request to receiving the first token of a streaming
	// response, in microseconds.
	//
	// Type: int
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: 1200
	GenAIResponseTimeToFirstTokenKey = attribute.Key("gen_ai.response.time_to_first_token")

	// GenAIUsageCacheCreationInputTokensKey is the attribute Key conforming to the
	// "gen_ai.usage.cache_creation.input_tokens" semantic conventions. It
	// represents the number of prompt tokens that were written to the provider's
	// prompt cache.
	//
	// Type: int
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: 512
	GenAIUsageCacheCreationInputTokensKey = attribute.Key("gen_ai.usage.cache_creation.input_tokens")

	// GenAIUsageCacheReadInputTokensKey is the attribute Key conforming to the
	// "gen_ai.usage.cache_read.input_tokens" semantic conventions. It represents
	// the number of prompt tokens that were read from the provider's prompt cache.
	//
	// Type: int
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: 1024
	GenAIUsageCacheReadInputTokensKey = attribute.Key("gen_ai.usage.cache_read.input_tokens")

	// GenAIUsageTotalTokensKey is the attribute Key conforming to the
	// "gen_ai.usage.total_tokens" semantic conventions. It represents the total
	// number of tokens used in the request and response.
	//
	// Type: int
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: 180
	GenAIUsageTotalTokensKey = attribute.Key("gen_ai.usage.total_tokens")
)

// GenAIRequestIsStream returns an attribute KeyValue conforming to the
// "gen_ai.request.is_stream" semantic conventions. It represents the whether the
// request was served as a streaming (server-sent events) response.
func GenAIRequestIsStream(val bool) attribute.KeyValue {
	return GenAIRequestIsStreamKey.Bool(val)
}

// GenAIResponseTimeToFirstToken returns an attribute KeyValue conforming to the
// "gen_ai.response.time_to_first_token" semantic conventions. It represents the
// time from sending the request to receiving the first token of a streaming
// response, in microseconds.
func GenAIResponseTimeToFirstToken(val int) attribute.KeyValue {
	return GenAIResponseTimeToFirstTokenKey.Int(val)
}

// GenAIUsageCacheCreationInputTokens returns an attribute KeyValue conforming to
// the "gen_ai.usage.cache_creation.input_tokens" semantic conventions. It
// represents the number of prompt tokens that were written to the provider's
// prompt cache.
func GenAIUsageCacheCreationInputTokens(val int) attribute.KeyValue {
	return GenAIUsageCacheCreationInputTokensKey.Int(val)
}

// GenAIUsageCacheReadInputTokens returns an attribute KeyValue conforming to the
// "gen_ai.usage.cache_read.input_tokens" semantic conventions. It represents the
// number of prompt tokens that were read from the provider's prompt cache.
func GenAIUsageCacheReadInputTokens(val int) attribute.KeyValue {
	return GenAIUsageCacheReadInputTokensKey.Int(val)
}

// GenAIUsageTotalTokens returns an attribute KeyValue conforming to the
// "gen_ai.usage.total_tokens" semantic conventions. It represents the total
// number of tokens used in the request and response.
func GenAIUsageTotalTokens(val int) attribute.KeyValue {
	return GenAIUsageTotalTokensKey.Int(val)
}

// Namespace: k8s
const (
	// K8SObjectAPIVersionKey is the attribute Key conforming to the
	// "k8s.object.api_version" semantic conventions. It represents the API version
	// (group/version) of the Kubernetes object.
	//
	// Type: string
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: "apps/v1", "v1"
	K8SObjectAPIVersionKey = attribute.Key("k8s.object.api_version")

	// K8SObjectKindKey is the attribute Key conforming to the "k8s.object.kind"
	// semantic conventions. It represents the kind of the Kubernetes object.
	//
	// Type: string
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: "Pod", "Deployment"
	K8SObjectKindKey = attribute.Key("k8s.object.kind")

	// K8SObjectNameKey is the attribute Key conforming to the "k8s.object.name"
	// semantic conventions. It represents the name of a Kubernetes object whose
	// concrete kind has no dedicated upstream attribute.
	//
	// Type: string
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: "my-object"
	K8SObjectNameKey = attribute.Key("k8s.object.name")

	// K8SObjectUIDKey is the attribute Key conforming to the "k8s.object.uid"
	// semantic conventions. It represents the UID of a Kubernetes object whose
	// concrete kind has no dedicated upstream attribute.
	//
	// Type: string
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: "275ecb36-5aa8-4c2a-9c47-d8bb681b9aff"
	K8SObjectUIDKey = attribute.Key("k8s.object.uid")

	// K8SObjectsCountKey is the attribute Key conforming to the "k8s.objects.count"
	// semantic conventions. It represents the number of objects processed in a
	// single informer batch.
	//
	// Type: int
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: 42
	K8SObjectsCountKey = attribute.Key("k8s.objects.count")

	// K8SObjectsIsInInitialListKey is the attribute Key conforming to the
	// "k8s.objects.is_in_initial_list" semantic conventions. It represents the
	// whether the processed objects are part of the informer's initial list (as
	// opposed to a subsequent watch event).
	//
	// Type: boolean
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples:
	K8SObjectsIsInInitialListKey = attribute.Key("k8s.objects.is_in_initial_list")
)

// K8SObjectAPIVersion returns an attribute KeyValue conforming to the
// "k8s.object.api_version" semantic conventions. It represents the API version
// (group/version) of the Kubernetes object.
func K8SObjectAPIVersion(val string) attribute.KeyValue {
	return K8SObjectAPIVersionKey.String(val)
}

// K8SObjectKind returns an attribute KeyValue conforming to the
// "k8s.object.kind" semantic conventions. It represents the kind of the
// Kubernetes object.
func K8SObjectKind(val string) attribute.KeyValue {
	return K8SObjectKindKey.String(val)
}

// K8SObjectName returns an attribute KeyValue conforming to the
// "k8s.object.name" semantic conventions. It represents the name of a Kubernetes
// object whose concrete kind has no dedicated upstream attribute.
func K8SObjectName(val string) attribute.KeyValue {
	return K8SObjectNameKey.String(val)
}

// K8SObjectUID returns an attribute KeyValue conforming to the "k8s.object.uid"
// semantic conventions. It represents the UID of a Kubernetes object whose
// concrete kind has no dedicated upstream attribute.
func K8SObjectUID(val string) attribute.KeyValue {
	return K8SObjectUIDKey.String(val)
}

// K8SObjectsCount returns an attribute KeyValue conforming to the
// "k8s.objects.count" semantic conventions. It represents the number of objects
// processed in a single informer batch.
func K8SObjectsCount(val int) attribute.KeyValue {
	return K8SObjectsCountKey.Int(val)
}

// K8SObjectsIsInInitialList returns an attribute KeyValue conforming to the
// "k8s.objects.is_in_initial_list" semantic conventions. It represents the
// whether the processed objects are part of the informer's initial list (as
// opposed to a subsequent watch event).
func K8SObjectsIsInInitialList(val bool) attribute.KeyValue {
	return K8SObjectsIsInInitialListKey.Bool(val)
}

// Namespace: messaging
const (
	// MessagingKafkaAsyncKey is the attribute Key conforming to the
	// "messaging.kafka.async" semantic conventions. It represents the whether the
	// message was sent through a kafka.Writer with Async enabled. When true, the
	// span only reflects local hand-off to the client library, not confirmed broker
	// delivery, so its duration and status must not be read as broker round-trip
	// latency or delivery success. Only ever emitted as `true`; omitted (not
	// `false`) for synchronous writes, where the span already reflects a confirmed
	// broker round-trip.
	//
	// Type: boolean
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples:
	MessagingKafkaAsyncKey = attribute.Key("messaging.kafka.async")
)

// MessagingKafkaAsync returns an attribute KeyValue conforming to the
// "messaging.kafka.async" semantic conventions. It represents the whether the
// message was sent through a kafka.Writer with Async enabled. When true, the
// span only reflects local hand-off to the client library, not confirmed broker
// delivery, so its duration and status must not be read as broker round-trip
// latency or delivery success. Only ever emitted as `true`; omitted (not `false`
// ) for synchronous writes, where the span already reflects a confirmed broker
// round-trip.
func MessagingKafkaAsync(val bool) attribute.KeyValue {
	return MessagingKafkaAsyncKey.Bool(val)
}

// Namespace: rpc
const (
	// RPCSystemNameKey is the attribute Key conforming to the "rpc.system.name"
	// semantic conventions. It represents the identifies the RPC system as the AWS
	// API. This is the merged form of the older upstream `rpc.system` attribute,
	// emitted by the newer semconv that otelaws tracks.
	//
	// Type: string
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: "aws-api"
	RPCSystemNameKey = attribute.Key("rpc.system.name")
)

// RPCSystemName returns an attribute KeyValue conforming to the
// "rpc.system.name" semantic conventions. It represents the identifies the RPC
// system as the AWS API. This is the merged form of the older upstream
// `rpc.system` attribute, emitted by the newer semconv that otelaws tracks.
func RPCSystemName(val string) attribute.KeyValue {
	return RPCSystemNameKey.String(val)
}

// Namespace: stripe
const (
	// StripeErrorCodeKey is the attribute Key conforming to the "stripe.error.code"
	// semantic conventions. It represents the Stripe error code, present on errors
	// that carry one.
	//
	// Type: string
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: "card_declined", "resource_missing"
	StripeErrorCodeKey = attribute.Key("stripe.error.code")

	// StripeErrorTypeKey is the attribute Key conforming to the "stripe.error.type"
	// semantic conventions. It represents the Stripe error type.
	//
	// Type: string
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: "card_error", "invalid_request_error", "api_error"
	StripeErrorTypeKey = attribute.Key("stripe.error.type")

	// StripeRequestIDKey is the attribute Key conforming to the "stripe.request_id"
	// semantic conventions. It represents the stripe's `Request-Id` for the call.
	// This is the identifier Stripe support and the dashboard's request log key
	// off, so it is the join key between a span and the request as Stripe recorded
	// it.
	//
	// Type: string
	// RequirementLevel: Recommended
	// Stability: Development
	//
	// Examples: "req_9tUAcSHnw5xQBK"
	StripeRequestIDKey = attribute.Key("stripe.request_id")
)

// StripeErrorCode returns an attribute KeyValue conforming to the
// "stripe.error.code" semantic conventions. It represents the Stripe error code,
// present on errors that carry one.
func StripeErrorCode(val string) attribute.KeyValue {
	return StripeErrorCodeKey.String(val)
}

// StripeErrorType returns an attribute KeyValue conforming to the
// "stripe.error.type" semantic conventions. It represents the Stripe error type.
func StripeErrorType(val string) attribute.KeyValue {
	return StripeErrorTypeKey.String(val)
}

// StripeRequestID returns an attribute KeyValue conforming to the
// "stripe.request_id" semantic conventions. It represents the stripe's
// `Request-Id` for the call. This is the identifier Stripe support and the
// dashboard's request log key off, so it is the join key between a span and the
// request as Stripe recorded it.
func StripeRequestID(val string) attribute.KeyValue {
	return StripeRequestIDKey.String(val)
}
