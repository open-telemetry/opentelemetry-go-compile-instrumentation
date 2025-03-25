// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package experimental

import (
	"go.opentelemetry.io/otel/metric"
	"log"
	"os"
)

var (
	ClientServiceInfoMapSize    metric.Int64ObservableGauge
	ClientConfigCacheMapSize    metric.Int64ObservableGauge
	ClientDomBeatMapSize        metric.Int64ObservableGauge
	ClientConfigRequestDuration metric.Float64Histogram
	ClientNamingRequestDuration metric.Float64Histogram
	GlobalMeter                 metric.Meter
)

type nacosEnabler struct{}

func (n nacosEnabler) Enable() bool {
	return os.Getenv("OTEL_INSTRUMENTATION_NACOS_EXPERIMENTAL_METRICS_ENABLE") == "true"
}

var NacosEnabler nacosEnabler

func InitNacosExperimentalMetrics(m metric.Meter) {
	GlobalMeter = m
	if GlobalMeter == nil {
		return
	}
	var err error
	ClientServiceInfoMapSize, err = GlobalMeter.Int64ObservableGauge("nacos.client.serviceinfo.size", metric.WithDescription("Size of service info map"))
	if err != nil {
		log.Printf("failed to init ClientServiceInfoMapSize metrics")
	}
	ClientConfigCacheMapSize, err = GlobalMeter.Int64ObservableGauge("nacos.client.configinfo.size", metric.WithDescription("Size of config cache map"))
	if err != nil {
		log.Printf("failed to init ClientConfigCacheMapSize metrics")
	}
	ClientDomBeatMapSize, err = GlobalMeter.Int64ObservableGauge("nacos.client.dombeat.size", metric.WithDescription("Size of dom beat map"))
	if err != nil {
		log.Printf("failed to init ClientDomBeatMapSize metrics")
	}
	ClientConfigRequestDuration, err = GlobalMeter.Float64Histogram("nacos.client.config.request.duration", metric.WithDescription("Duration of config request"))
	if err != nil {
		log.Printf("failed to init ClientConfigRequestDuration metrics")
	}
	ClientNamingRequestDuration, err = GlobalMeter.Float64Histogram("nacos.client.naming.request.duration", metric.WithDescription("Duration of naming request"))
	if err != nil {
		log.Printf("failed to init ClientNamingRequestDuration metrics")
	}
}
