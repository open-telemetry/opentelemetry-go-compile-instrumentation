// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package net

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

type MockURLGetter struct{}

func (*MockURLGetter) GetURLScheme(_ any) string {
	return "http"
}

func (*MockURLGetter) GetURLPath(_ any) string {
	return "/test"
}

func (*MockURLGetter) GetURLQuery(_ any) string {
	return "key=value"
}

func TestOnStart(t *testing.T) {
	mockGetter := &MockURLGetter{}
	urlExtractor := &URLAttrsExtractor[any, any, *MockURLGetter]{
		Getter: mockGetter,
	}
	parentContext := context.Background()
	attributes := []attribute.KeyValue{
		attribute.String("existingKey", "existingValue"),
	}
	urlExtractor.OnEnd(context.Background(), make([]attribute.KeyValue, 0), nil, nil, nil)
	resultAttributes, resultContext := urlExtractor.OnStart(parentContext, attributes, nil)
	expectedAttributes := []attribute.KeyValue{
		attribute.String("existingKey", "existingValue"),
		attribute.String(string(semconv.URLSchemeKey), "http"),
		attribute.String(string(semconv.URLPathKey), "/test"),
		attribute.String(string(semconv.URLQueryKey), "key=value"),
	}
	assert.Equal(t, expectedAttributes, resultAttributes)
	assert.Equal(t, parentContext, resultContext)
}
