// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestHTTPMetricNamesMatchRegistry(t *testing.T) {
	declared := declaredHTTPMetricNames(t)
	registered := append(NewHTTPClient(nil).metricNames(), NewHTTPServer(nil).metricNames()...)

	assert.ElementsMatch(t, declared, registered)
}

func declaredHTTPMetricNames(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile("../../../../schemas/otelc/groups/http.yaml")
	require.NoError(t, err)

	var registry struct {
		Groups []struct {
			MetricName string `yaml:"metric_name"`
		} `yaml:"groups"`
	}
	require.NoError(t, yaml.Unmarshal(data, &registry))

	names := make([]string, 0, len(registry.Groups))
	for i, group := range registry.Groups {
		require.NotEmpty(t, group.MetricName, "group %d has no metric_name", i)
		names = append(names, group.MetricName)
	}
	return names
}
