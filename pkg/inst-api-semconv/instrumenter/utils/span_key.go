// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import "go.opentelemetry.io/otel/attribute"

const (
	HTTPClientKey = attribute.Key("opentelemetry-traces-span-key-http-client")
	HTTPServerKey = attribute.Key("opentelemetry-traces-span-key-http-server")

	ClientResendKey = attribute.Key("opentelemetry-http-client-resend-key")
)
