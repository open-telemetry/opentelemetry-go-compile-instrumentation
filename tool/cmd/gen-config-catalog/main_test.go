// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/configuration"
)

func TestRunWritesGeneratedCatalog(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	previous := filepath.Join(t.TempDir(), "previous.yaml")
	previousData := []byte(`catalog_version: 1
source:
  repository: open-telemetry/opentelemetry-configuration
  release: v1.2.0
  commit: 3b04ae78576ec407854d734eb4da4f78a67f902c
  file: schema/instrumentation.yaml
definitions:
  general.http.client.request_captured_headers:
    name: general.http.client.request_captured_headers
    type: list
`)
	require.NoError(t, os.WriteFile(previous, previousData, 0o600))
	upstream := []byte(`properties:
  general:
    properties:
      http:
        properties:
          client:
            properties:
              request_captured_headers:
                type: array
                items: {type: string}
`)
	var stderr bytes.Buffer

	code := runWithFetcher([]string{"-previous", previous}, &stderr,
		func(context.Context) ([]byte, error) { return upstream, nil })

	assert.Zero(t, code)
	assert.Empty(t, stderr.String())
	written, err := os.ReadFile(filepath.Join(workDir, "catalog.yaml"))
	require.NoError(t, err)
	goOptions, err := generatorInputs.ReadFile("go-configurations.yaml")
	require.NoError(t, err)
	compatibility, err := generatorInputs.ReadFile("general-compatibility.yaml")
	require.NoError(t, err)
	expected, err := configuration.GenerateCatalog(upstream, goOptions, compatibility, previousData)
	require.NoError(t, err)
	assert.Equal(t, expected, written)
}

func TestRunReportsFlagErrors(t *testing.T) {
	var stderr bytes.Buffer

	code := runWithFetcher([]string{"-unknown"}, &stderr, nil)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "flag provided but not defined: -unknown")
}

func TestRunReportsFetcherErrors(t *testing.T) {
	var stderr bytes.Buffer

	code := runWithFetcher(nil, &stderr, func(context.Context) ([]byte, error) {
		return nil, errors.New("upstream unavailable")
	})

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "upstream unavailable")
}

func TestRunReportsPreviousCatalogErrors(t *testing.T) {
	var stderr bytes.Buffer

	code := runWithFetcher([]string{"-previous", filepath.Join(t.TempDir(), "missing.yaml")}, &stderr,
		func(context.Context) ([]byte, error) { return testUpstreamSchema(), nil })

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "read previous catalog")
}

func TestRunReportsGeneratorInputErrors(t *testing.T) {
	for _, input := range []string{"go-configurations.yaml", "general-compatibility.yaml"} {
		t.Run(input, func(t *testing.T) {
			var stderr bytes.Buffer
			code := runWithFetcherAndInput(nil, &stderr,
				func(context.Context) ([]byte, error) { return testUpstreamSchema(), nil },
				func(name string) ([]byte, error) {
					if name == input {
						return nil, errors.New("fixture unavailable")
					}
					return []byte("configurations: []\n"), nil
				})
			assert.Equal(t, 1, code)
			assert.Contains(t, stderr.String(), "fixture unavailable")
		})
	}
}

func TestRunReportsCatalogGenerationErrors(t *testing.T) {
	previous := filepath.Join(t.TempDir(), "invalid.yaml")
	require.NoError(t, os.WriteFile(previous, []byte("not: a catalog"), 0o600))
	var stderr bytes.Buffer

	code := runWithFetcher([]string{"-previous", previous}, &stderr,
		func(context.Context) ([]byte, error) { return testUpstreamSchema(), nil })

	assert.Equal(t, 1, code)
	assert.NotEmpty(t, stderr.String())
}

func TestRunShowsHelpWithoutError(t *testing.T) {
	var stderr bytes.Buffer

	code := runWithFetcher([]string{"-h"}, &stderr, nil)

	assert.Zero(t, code)
	assert.Contains(t, stderr.String(), "previous catalog")
}

func TestRunReportsOutputErrors(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	require.NoError(t, os.Mkdir(filepath.Join(workDir, "catalog.yaml"), 0o700))
	previous := filepath.Join(t.TempDir(), "previous.yaml")
	previousData := testPreviousCatalog()
	require.NoError(t, os.WriteFile(previous, previousData, 0o600))
	var stderr bytes.Buffer

	code := runWithFetcher(
		[]string{"-previous", previous},
		&stderr,
		func(context.Context) ([]byte, error) { return testUpstreamSchema(), nil },
	)

	assert.Equal(t, 1, code)
	assert.NotEmpty(t, stderr.String())
}

func TestVerifyPinnedSchema(t *testing.T) {
	require.ErrorContains(t, verifyPinnedSchema([]byte("different schema")), "checksum mismatch")
}

func TestFetchSchemaVerifiesResponseAndChecksum(t *testing.T) {
	body := []byte("schema fixture")
	wantSHA := fmt.Sprintf("%x", sha256.Sum256(body))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	got, err := fetchSchema(t.Context(), server.Client(), server.URL, wantSHA)
	require.NoError(t, err)
	assert.Equal(t, body, got)
	assert.ErrorContains(t, verifySchemaChecksum(body, "wrong checksum"), "checksum mismatch")
}

func TestFetchSchemaRejectsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	_, err := fetchSchema(t.Context(), server.Client(), server.URL, upstreamSchemaSHA)
	require.ErrorContains(t, err, "unexpected HTTP status")
}

func TestFetchSchemaRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxSchemaSize+1))
	}))
	defer server.Close()

	_, err := fetchSchema(t.Context(), server.Client(), server.URL, upstreamSchemaSHA)
	require.ErrorContains(t, err, "exceeds")
}

func TestFetchSchemaRejectsChecksumMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("schema fixture"))
	}))
	defer server.Close()

	_, err := fetchSchema(t.Context(), server.Client(), server.URL, "wrong checksum")
	require.ErrorContains(t, err, "checksum mismatch")
}

func TestFetchSchemaRejectsInvalidURL(t *testing.T) {
	_, err := fetchSchema(t.Context(), http.DefaultClient, "://invalid", upstreamSchemaSHA)
	require.ErrorContains(t, err, "create upstream schema request")
}

func TestFetchPinnedSchemaReportsCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := fetchPinnedSchema(ctx)
	require.ErrorContains(t, err, "fetch pinned upstream schema")
}

func TestFetchSchemaReportsTransportError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})}

	_, err := fetchSchema(t.Context(), client, "https://example.com/schema", upstreamSchemaSHA)
	require.ErrorContains(t, err, "fetch pinned upstream schema")
}

func TestFetchSchemaReportsReadError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(errorReader{}),
		}, nil
	})}

	_, err := fetchSchema(t.Context(), client, "https://example.com/schema", upstreamSchemaSHA)
	require.ErrorContains(t, err, "read pinned upstream schema")
}

func TestWriteGeneratedCatalogReportsRootErrors(t *testing.T) {
	err := writeGeneratedCatalog(filepath.Join(t.TempDir(), "missing", "catalog.yaml"), []byte("catalog"))
	require.ErrorContains(t, err, "open catalog output root")
}

func TestWriteGeneratedCatalogReportsWriteErrors(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full is unavailable")
	}
	err := writeGeneratedCatalog("/dev/full", []byte("catalog"))
	require.ErrorContains(t, err, "write /dev/full")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func testUpstreamSchema() []byte {
	return []byte(`properties:
  general:
    properties:
      http:
        properties:
          client:
            properties:
              request_captured_headers:
                type: array
                items: {type: string}
`)
}

func testPreviousCatalog() []byte {
	return []byte(`catalog_version: 1
source:
  repository: open-telemetry/opentelemetry-configuration
  release: v1.2.0
  commit: 3b04ae78576ec407854d734eb4da4f78a67f902c
  file: schema/instrumentation.yaml
definitions:
  general.http.client.request_captured_headers:
    name: general.http.client.request_captured_headers
    type: list
`)
}
