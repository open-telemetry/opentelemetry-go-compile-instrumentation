//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package clickhouse

import (
	"context"
	_ "unsafe"
)

// This file is added to package clickhouse at compile time. The hooks receive
// the unexported *clickhouse, *batch and *httpBatch receivers as `any` and
// cannot read their fields, so it hands the hook package two accessors.

//go:linkname otelcSetAccessors go.opentelemetry.io/otelc/instrumentation/github.com/ClickHouse/clickhouse-go/v2.setAccessors
func otelcSetAccessors(options func(any) *Options, batchContext func(any) context.Context)

func init() {
	otelcSetAccessors(
		func(conn any) *Options {
			if c, ok := conn.(*clickhouse); ok && c != nil {
				return c.opt
			}
			return nil
		},
		func(b any) context.Context {
			switch b := b.(type) {
			case *batch:
				if b != nil {
					return b.ctx
				}
			case *httpBatch:
				if b != nil {
					return b.ctx
				}
			}
			return nil
		},
	)
}
