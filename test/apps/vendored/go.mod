// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

module go.opentelemetry.io/otelc/test/apps/vendored

go 1.26.0

require (
	go.opentelemetry.io/otelc/test/apps/vendored/hooks v0.0.0
	golang.org/x/time v0.16.0
)

require go.opentelemetry.io/otelc/pkg v0.0.0 // indirect

replace (
	go.opentelemetry.io/otelc/pkg => ../../../pkg
	go.opentelemetry.io/otelc/test/apps/vendored/hooks => ./hooks
)
