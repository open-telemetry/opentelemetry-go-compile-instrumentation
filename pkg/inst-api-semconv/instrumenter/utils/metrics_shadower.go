// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"go.opentelemetry.io/otel/attribute"
)

/**
We only need to record some of the attributes in the metrics, since some of the attributes are high-cardinality.
So we use this function to shadow the attributes that are not needed in the metrics.
*/

func Shadow(attrs []attribute.KeyValue, metricsSemConv map[attribute.Key]bool) (int, []attribute.KeyValue) {
	swap := func(attrs []attribute.KeyValue, i, j int) {
		tmp := attrs[i]
		attrs[i] = attrs[j]
		attrs[j] = tmp
	}
	index := 0
	for i, attr := range attrs {
		if _, ok := metricsSemConv[attr.Key]; ok {
			if index != i {
				swap(attrs, i, index)
			}
			index++
		}
	}
	return index, attrs
}
