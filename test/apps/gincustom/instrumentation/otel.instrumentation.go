// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build tools

package tools

import (
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/net/http/server" // enable net/http server instrumentation
)
