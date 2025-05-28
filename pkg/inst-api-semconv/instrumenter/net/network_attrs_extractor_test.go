// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package net

import (
	"context"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"testing"
)

type MockUrlGetter struct{}

func (m *MockUrlGetter) GetUrlScheme(request any) string {
	return "http"
}

func (m *MockUrlGetter) GetUrlPath(request any) string {
	return "/test"
}

func (m *MockUrlGetter) GetUrlQuery(request any) string {
	return "key=value"
}

func TestOnStart(t *testing.T) {
	mockGetter := &MockUrlGetter{}
	urlExtractor := &UrlAttrsExtractor[any, any, *MockUrlGetter]{
		Getter: mockGetter,
	}
	parentContext := context.Background()
	attributes := []attribute.KeyValue{
		attribute.String("existingKey", "existingValue"),
	}
	urlExtractor.OnEnd(context.Background(), []attribute.KeyValue{}, nil, nil, nil)
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
