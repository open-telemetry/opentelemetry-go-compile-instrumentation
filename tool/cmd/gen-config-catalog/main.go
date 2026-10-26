// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Command gen-config-catalog regenerates the checked-in catalog without network access.
package main

import (
	"flag"
	"fmt"
	"os"

	"go.opentelemetry.io/otelc/tool/internal/configuration"
)

func main() {
	output := flag.String("output", "catalog.yaml", "destination catalog path")
	flag.Parse()
	data, err := configuration.GenerateCatalog()
	if err == nil {
		err = os.WriteFile(*output, data, 0o600)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
