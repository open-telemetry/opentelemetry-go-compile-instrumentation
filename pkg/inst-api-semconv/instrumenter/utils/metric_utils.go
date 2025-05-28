// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

func NewFloat64Histogram(metricName, metricUnit, metricDescription string,
	meter metric.Meter,
) (metric.Float64Histogram, error) {
	if meter == nil {
		return nil, errors.New("nil meter")
	}
	d, err := meter.Float64Histogram(metricName,
		metric.WithUnit(metricUnit),
		metric.WithDescription(metricDescription))
	if err == nil {
		return d, nil
	}
	return d, fmt.Errorf("failed to create %s histogram, %w", metricName, err)
}
