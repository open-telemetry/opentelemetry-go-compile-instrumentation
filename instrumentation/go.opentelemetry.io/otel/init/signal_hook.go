//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package signal

import (
	"os"
	_ "unsafe"
)

// otelcRecordNotify is reached by linkname rather than by importing
// pkg/runtime: pkg/runtime imports os/signal, so an import edge from this
// injected file would close a cycle.
//
//go:linkname otelcRecordNotify go.opentelemetry.io/otelc/pkg/runtime.RecordSignalNotify
func otelcRecordNotify(c chan<- os.Signal, sig []os.Signal)
