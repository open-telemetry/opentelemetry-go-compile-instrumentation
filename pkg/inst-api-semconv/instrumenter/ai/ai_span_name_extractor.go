// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ai

type AISpanNameExtractor[REQUEST any, RESPONSE any] struct {
	Getter CommonAttrsGetter[REQUEST, RESPONSE]
}

func (d *AISpanNameExtractor[REQUEST, RESPONSE]) Extract(request REQUEST) string {
	operation := d.Getter.GetAIOperationName(request)
	if operation == "" {
		return "unknown"
	}
	return operation
}
