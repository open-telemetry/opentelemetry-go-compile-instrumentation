// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package propagation

import (
	"testing"

	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
)

func TestHeaderCarrier_Get(t *testing.T) {
	headers := []kafka.Header{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Value: []byte("value2")},
	}
	carrier := NewHeaderCarrier(&headers)

	assert.Equal(t, "value1", carrier.Get("key1"))
	assert.Equal(t, "value2", carrier.Get("key2"))
	assert.Equal(t, "", carrier.Get("nonexistent"))
}

func TestHeaderCarrier_Set_ReplaceExisting(t *testing.T) {
	headers := []kafka.Header{
		{Key: "key1", Value: []byte("value1")},
	}
	carrier := NewHeaderCarrier(&headers)

	carrier.Set("key1", "newvalue")

	assert.Len(t, headers, 1)
	assert.Equal(t, "key1", headers[0].Key)
	assert.Equal(t, []byte("newvalue"), headers[0].Value)
}

func TestHeaderCarrier_Set_AppendNew(t *testing.T) {
	headers := []kafka.Header{
		{Key: "key1", Value: []byte("value1")},
	}
	carrier := NewHeaderCarrier(&headers)

	carrier.Set("key2", "value2")

	assert.Len(t, headers, 2)
	assert.Equal(t, "key2", headers[1].Key)
	assert.Equal(t, []byte("value2"), headers[1].Value)
}

func TestHeaderCarrier_Keys(t *testing.T) {
	headers := []kafka.Header{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Value: []byte("value2")},
		{Key: "key3", Value: []byte("value3")},
	}
	carrier := NewHeaderCarrier(&headers)

	keys := carrier.Keys()

	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "key1")
	assert.Contains(t, keys, "key2")
	assert.Contains(t, keys, "key3")
}

func TestHeaderCarrier_Keys_Empty(t *testing.T) {
	headers := []kafka.Header{}
	carrier := NewHeaderCarrier(&headers)

	keys := carrier.Keys()

	assert.Len(t, keys, 0)
}
