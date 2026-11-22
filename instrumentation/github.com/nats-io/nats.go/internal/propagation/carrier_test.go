// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package propagation

import (
	"testing"

	nats "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
)

func TestHeaderCarrier(t *testing.T) {
	t.Run("nil carrier", func(t *testing.T) {
		var carrier HeaderCarrier
		assert.Empty(t, carrier.Get("traceparent"))
		assert.Nil(t, carrier.Keys())
		// Set on nil carrier should not panic
		carrier.Set("traceparent", "00-test-01")
	})

	t.Run("get, set and keys", func(t *testing.T) {
		h := make(nats.Header)
		carrier := NewHeaderCarrier(h)

		carrier.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
		carrier.Set("baggage", "userId=alice")

		assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", carrier.Get("traceparent"))
		assert.Equal(t, "userId=alice", carrier.Get("baggage"))
		assert.Empty(t, carrier.Get("nonexistent"))

		keys := carrier.Keys()
		assert.ElementsMatch(t, []string{"traceparent", "baggage"}, keys)
	})
}
