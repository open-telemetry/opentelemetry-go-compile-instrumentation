// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/configuration"
)

func TestRunWritesGeneratedCatalog(t *testing.T) {
	output := filepath.Join(t.TempDir(), "catalog.yaml")
	var stderr bytes.Buffer

	code := run([]string{"-output", output}, &stderr)

	assert.Zero(t, code)
	assert.Empty(t, stderr.String())
	written, err := os.ReadFile(output)
	require.NoError(t, err)
	expected, err := configuration.GenerateCatalog()
	require.NoError(t, err)
	assert.Equal(t, expected, written)
}

func TestRunReportsFlagErrors(t *testing.T) {
	var stderr bytes.Buffer

	code := run([]string{"-unknown"}, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "flag provided but not defined: -unknown")
}

func TestRunShowsHelpWithoutError(t *testing.T) {
	var stderr bytes.Buffer

	code := run([]string{"-h"}, &stderr)

	assert.Zero(t, code)
	assert.Contains(t, stderr.String(), "destination catalog path")
}

func TestRunReportsOutputErrors(t *testing.T) {
	outputDir := t.TempDir()
	var stderr bytes.Buffer

	code := run([]string{"-output", outputDir}, &stderr)

	assert.Equal(t, 1, code)
	assert.NotEmpty(t, stderr.String())
}
