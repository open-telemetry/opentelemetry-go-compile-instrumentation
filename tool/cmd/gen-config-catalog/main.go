// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Command gen-config-catalog regenerates the checked-in catalog without network access.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"go.opentelemetry.io/otelc/tool/internal/configuration"
)

const flagErrorExitCode = 2

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("gen-config-catalog", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "catalog.yaml", "destination catalog path")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return flagErrorExitCode
	}

	data, err := configuration.GenerateCatalog()
	if err == nil {
		err = os.WriteFile(*output, data, 0o600)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
