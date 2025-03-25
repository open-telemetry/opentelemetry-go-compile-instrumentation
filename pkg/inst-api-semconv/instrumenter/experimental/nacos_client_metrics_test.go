// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package experimental

import (
	"go.opentelemetry.io/otel/sdk/metric"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitNacosExperimentalMetrics_GlobalMeterNil_NoMetricsInitialized(t *testing.T) {
	InitNacosExperimentalMetrics(nil)
	assert.Nil(t, ClientServiceInfoMapSize)
	assert.Nil(t, ClientConfigCacheMapSize)
	assert.Nil(t, ClientDomBeatMapSize)
	assert.Nil(t, ClientConfigRequestDuration)
	assert.Nil(t, ClientNamingRequestDuration)
}

func TestInitNacosExperimentalMetrics_GlobalMeterNotNull_AllMetricsInitialized(t *testing.T) {
	mp := metric.NewMeterProvider()
	InitNacosExperimentalMetrics(mp.Meter("a"))
	assert.NotNil(t, ClientServiceInfoMapSize)
	assert.NotNil(t, ClientConfigCacheMapSize)
	assert.NotNil(t, ClientDomBeatMapSize)
	assert.NotNil(t, ClientConfigRequestDuration)
	assert.NotNil(t, ClientNamingRequestDuration)
}

func TestNacosEnablerDisable(t *testing.T) {
	ne := nacosEnabler{}
	if ne.Enable() {
		panic("should not enable without OTEL_INSTRUMENTATION_NACOS_EXPERIMENTAL_METRICS_ENABLE")
	}
}

func TestNacosEnablerEnable(t *testing.T) {
	os.Setenv("OTEL_INSTRUMENTATION_NACOS_EXPERIMENTAL_METRICS_ENABLE", "true")
	ne := nacosEnabler{}
	if !ne.Enable() {
		panic("should enable with OTEL_INSTRUMENTATION_NACOS_EXPERIMENTAL_METRICS_ENABLE")
	}
}
