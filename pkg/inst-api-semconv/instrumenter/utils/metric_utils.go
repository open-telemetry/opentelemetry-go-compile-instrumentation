// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"errors"
	"fmt"
	"go.opentelemetry.io/otel/metric"
	"sync"
)

var mu sync.Mutex

func NewFloat64Histogram(metricName, metricUnit, metricDescription string,
	meter metric.Meter) (metric.Float64Histogram, error) {
	mu.Lock()
	defer mu.Unlock()
	if meter == nil {
		return nil, errors.New("nil meter")
	}
	d, err := meter.Float64Histogram(metricName,
		metric.WithUnit(metricUnit),
		metric.WithDescription(metricDescription))
	if err == nil {
		return d, nil
	} else {
		return d, fmt.Errorf("failed to create %s histogram, %v", metricName, err)
	}
}
