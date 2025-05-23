// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import "go.opentelemetry.io/otel/attribute"

const HTTP_CLIENT_KEY = attribute.Key("opentelemetry-traces-span-key-http-client")
const HTTP_SERVER_KEY = attribute.Key("opentelemetry-traces-span-key-http-server")

const CLIENT_RESEND_KEY = attribute.Key("opentelemetry-http-client-resend-key")
