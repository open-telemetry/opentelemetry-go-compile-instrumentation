// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/openai/semconv"
)

// operationUnknown is used when a request is routed through the middleware
// but does not match any known OpenAI endpoint path. The span is still
// created so operators see the HTTP activity; only the operation attribute
// is generic.
const operationUnknown = "unknown"

// parseRoute maps an HTTP URL path to a GenAI operation name. It uses suffix
// matching so that both standard OpenAI paths (e.g. /v1/chat/completions)
// and Azure OpenAI deployment paths (e.g. /openai/deployments/{name}/chat/completions)
// resolve to the same operation.
func parseRoute(path string) string {
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return semconv.OperationChat
	default:
		return operationUnknown
	}
}
