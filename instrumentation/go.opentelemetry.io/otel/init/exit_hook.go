//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package os

import (
	_ "unsafe"
)

// otelcShutdownOnExit is reached by linkname rather than by importing
// pkg/runtime: pkg/runtime imports os, so an import edge from this injected
// file would close a cycle.
//
//go:linkname otelcShutdownOnExit go.opentelemetry.io/otelc/pkg/runtime.ShutdownOnExit
func otelcShutdownOnExit()
