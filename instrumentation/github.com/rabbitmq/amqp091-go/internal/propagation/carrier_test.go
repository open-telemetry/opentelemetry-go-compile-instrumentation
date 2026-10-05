// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package propagation

import (
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCarrier_SetGet(t *testing.T) {
	table := amqp.Table{}
	c := NewTableCarrier(&table)
	c.Set("traceparent", "00-abc")
	assert.Equal(t, "00-abc", c.Get("traceparent"))
	assert.Contains(t, c.Keys(), "traceparent")
}

func TestTableCarrier_AllocatesNilTable(t *testing.T) {
	var table amqp.Table
	c := NewTableCarrier(&table)
	c.Set("traceparent", "00-abc")
	require.NotNil(t, table)
	assert.Equal(t, "00-abc", c.Get("traceparent"))
}

func TestTableCarrier_Missing(t *testing.T) {
	table := amqp.Table{"other": "x"}
	c := NewTableCarrier(&table)
	assert.Empty(t, c.Get("traceparent"))
}

func TestTableCarrier_NonStringIgnored(t *testing.T) {
	table := amqp.Table{"traceparent": 1}
	c := NewTableCarrier(&table)
	assert.Empty(t, c.Get("traceparent"))
}
