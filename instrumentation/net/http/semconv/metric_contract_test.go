// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTTPMetricNamesMatchRegistry(t *testing.T) {
	declared := declaredHTTPMetricNames(t)
	registered := append(NewHTTPClient(nil).metricNames(), NewHTTPServer(nil).metricNames()...)

	assert.ElementsMatch(t, declared, registered)
}

func declaredHTTPMetricNames(t *testing.T) []string {
	t.Helper()

	file, err := os.Open("../../../../schemas/otelc/groups/http.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var names []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if name, ok := strings.CutPrefix(line, "metric_name:"); ok {
			names = append(names, strings.TrimSpace(name))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}
