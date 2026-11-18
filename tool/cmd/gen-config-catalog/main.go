// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Command gen-config-catalog regenerates the checked-in catalog from a pinned,
// checksum-verified OpenTelemetry Configuration schema.
package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otelc/tool/internal/configuration"
)

const (
	flagErrorExitCode = 2

	maxSchemaSize      = 8 << 20
	generatedFilePerm  = 0o600
	upstreamSchemaSHA  = "a6e45b6e5f6eb647f9101b5e045619b79368c83b561629e522e7c536775403bb"
	schemaFetchTimeout = 30 * time.Second
)

//go:embed go-configurations.yaml general-compatibility.yaml
var generatorInputs embed.FS

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	return runWithFetcher(args, stderr, fetchPinnedSchema)
}

func runWithFetcher(args []string, stderr io.Writer, fetcher func(context.Context) ([]byte, error)) int {
	return runWithFetcherAndInput(args, stderr, fetcher, generatorInputs.ReadFile)
}

func runWithFetcherAndInput(
	args []string,
	stderr io.Writer,
	fetcher func(context.Context) ([]byte, error),
	readInput func(string) ([]byte, error),
) int {
	flags := flag.NewFlagSet("gen-config-catalog", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "catalog.yaml", "destination catalog path")
	previous := flags.String("previous", "catalog.yaml", "previous catalog used to retain removed definitions")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return flagErrorExitCode
	}

	ctx, cancel := context.WithTimeout(context.Background(), schemaFetchTimeout)
	defer cancel()
	upstream, err := fetcher(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	goOptions, err := readInput("go-configurations.yaml")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	compatibility, err := readInput("general-compatibility.yaml")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	previousCatalog, err := os.ReadFile(*previous)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "read previous catalog %s: %v\n", *previous, err)
		return 1
	}
	data, err := configuration.GenerateCatalog(upstream, goOptions, compatibility, previousCatalog)
	if err == nil {
		err = writeGeneratedCatalog(*output, data)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func writeGeneratedCatalog(output string, data []byte) error {
	directory, filename := filepath.Split(output)
	if directory == "" {
		directory = "."
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return fmt.Errorf("open catalog output root: %w", err)
	}
	defer root.Close()
	file, err := root.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, generatedFilePerm)
	if err != nil {
		return fmt.Errorf("open %s for writing: %w", output, err)
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", output, err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", output, err)
	}
	return nil
}

func fetchPinnedSchema(ctx context.Context) ([]byte, error) {
	url := "https://raw.githubusercontent.com/" + configuration.OpenTelemetryConfigurationRepository + "/" +
		configuration.OpenTelemetryConfigurationCommit + "/" + configuration.OpenTelemetryConfigurationFile
	return fetchSchema(ctx, http.DefaultClient, url, upstreamSchemaSHA)
}

func fetchSchema(ctx context.Context, client *http.Client, url, expectedSHA string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create upstream schema request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch pinned upstream schema %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch pinned upstream schema %s: unexpected HTTP status %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSchemaSize+1))
	if err != nil {
		return nil, fmt.Errorf("read pinned upstream schema %s: %w", url, err)
	}
	if len(data) > maxSchemaSize {
		return nil, fmt.Errorf("pinned upstream schema exceeds %d bytes", maxSchemaSize)
	}
	err = verifySchemaChecksum(data, expectedSHA)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func verifyPinnedSchema(data []byte) error {
	return verifySchemaChecksum(data, upstreamSchemaSHA)
}

func verifySchemaChecksum(data []byte, expectedSHA string) error {
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != expectedSHA {
		return fmt.Errorf("pinned upstream schema checksum mismatch: got %s, want %s", got, expectedSHA)
	}
	return nil
}
