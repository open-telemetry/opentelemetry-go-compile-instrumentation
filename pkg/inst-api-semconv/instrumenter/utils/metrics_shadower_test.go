// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"testing"
)

func TestShadowEmptyInputReturnsZeroAndEmptySlice(t *testing.T) {
	attrs := []attribute.KeyValue{}
	metricsSemConv := map[attribute.Key]bool{}
	index, result := Shadow(attrs, metricsSemConv)
	assert.Equal(t, 0, index)
	assert.Empty(t, result)
}

func TestShadowNoMatchingKeysReturnsZeroAndUnchangedSlice(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("key1", "value1"),
		attribute.String("key2", "value2"),
	}
	metricsSemConv := map[attribute.Key]bool{}
	index, result := Shadow(attrs, metricsSemConv)
	assert.Equal(t, 0, index)
	assert.Equal(t, attrs, result)
}

func TestShadowAllKeysMatchingReturnsAllAndUnchangedSlice(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("key1", "value1"),
		attribute.String("key2", "value2"),
	}
	metricsSemConv := map[attribute.Key]bool{
		"key1": true,
		"key2": true,
	}
	index, result := Shadow(attrs, metricsSemConv)
	assert.Equal(t, 2, index)
	assert.Equal(t, attrs, result)
}

func TestShadowPartialMatchingKeysReturnsCorrectIndexAndReorderedSlice(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("key1", "value1"),
		attribute.String("key2", "value2"),
		attribute.String("key3", "value3"),
	}
	metricsSemConv := map[attribute.Key]bool{
		"key1": true,
		"key3": true,
	}
	expectedResult := []attribute.KeyValue{
		attribute.String("key1", "value1"),
		attribute.String("key3", "value3"),
		attribute.String("key2", "value2"),
	}
	index, result := Shadow(attrs, metricsSemConv)
	assert.Equal(t, 2, index)
	assert.Equal(t, expectedResult, result)
}

func TestShadowDuplicateKeysReturnsCorrectIndexAndReorderedSlice(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("key1", "value1"),
		attribute.String("key2", "value2"),
		attribute.String("key1", "value3"),
	}
	metricsSemConv := map[attribute.Key]bool{
		"key1": true,
	}
	expectedResult := []attribute.KeyValue{
		attribute.String("key1", "value1"),
		attribute.String("key1", "value3"),
		attribute.String("key2", "value2"),
	}
	index, result := Shadow(attrs, metricsSemConv)
	assert.Equal(t, 2, index)
	assert.Equal(t, expectedResult, result)
}
