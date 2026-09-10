// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package propagation

import (
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/propagation"
)

var _ propagation.TextMapCarrier = TableCarrier{}

// TableCarrier adapts an AMQP table to a TextMapCarrier so W3C trace
// context can ride on Publishing.Headers / Delivery.Headers.
type TableCarrier struct {
	table *amqp.Table
}

// NewTableCarrier wraps headers. A nil table pointer is not valid.
func NewTableCarrier(table *amqp.Table) TableCarrier {
	return TableCarrier{table: table}
}

// Get returns the string value for key, or "".
func (c TableCarrier) Get(key string) string {
	if c.table == nil || *c.table == nil {
		return ""
	}
	v, ok := (*c.table)[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Set writes key to the table, allocating the map when needed.
func (c TableCarrier) Set(key, value string) {
	if c.table == nil {
		return
	}
	if *c.table == nil {
		*c.table = amqp.Table{}
	}
	(*c.table)[key] = value
}

// Keys lists header keys.
func (c TableCarrier) Keys() []string {
	if c.table == nil || *c.table == nil {
		return nil
	}
	keys := make([]string, 0, len(*c.table))
	for k := range *c.table {
		keys = append(keys, k)
	}
	return keys
}
