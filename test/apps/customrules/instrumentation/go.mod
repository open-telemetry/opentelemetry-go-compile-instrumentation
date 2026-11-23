// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

module go.opentelemetry.io/otelc/test/apps/customrules/instrumentation

go 1.26.0

require (
	go.opentelemetry.io/otelc/pkg v0.0.0
	go.opentelemetry.io/otelc/test/apps/customrules/base v0.0.0
)

replace (
	go.opentelemetry.io/otelc/pkg => ../../../../pkg
	go.opentelemetry.io/otelc/test/apps/customrules/base => ../base
)
