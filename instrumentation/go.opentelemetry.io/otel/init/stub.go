// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This file keeps the package importable. The implementation lives in files
// marked with `//go:build ignore`, which are consumed by otelc during
// instrumentation.
package init

// import the runtime package to ensure it stays in go.mod
import (
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)
