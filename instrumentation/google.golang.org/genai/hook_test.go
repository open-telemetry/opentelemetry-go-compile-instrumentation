// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package genai

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	genaisdk "google.golang.org/genai"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

// newTestClient builds a Gemini API client without reaching the network. A base
// URL suppresses endpoint resolution and an API key keeps NewClient off the
// application-default-credentials path.
func newTestClient(t *testing.T, backend genaisdk.Backend, httpClient *http.Client) *genaisdk.Client {
	t.Helper()

	config := &genaisdk.ClientConfig{
		APIKey:      "test-key",
		Backend:     backend,
		HTTPClient:  httpClient,
		HTTPOptions: genaisdk.HTTPOptions{BaseURL: "http://127.0.0.1:0"},
	}
	if backend == genaisdk.BackendVertexAI {
		config.Project = "test-project"
		config.Location = "us-central1"
	}

	client, err := genaisdk.NewClient(t.Context(), config)
	require.NoError(t, err)

	return client
}

func TestProviderAndSystem(t *testing.T) {
	provider, system := providerAndSystem(genaisdk.BackendVertexAI)
	assert.Equal(t, providerVertexAI, provider)
	assert.Equal(t, systemVertexAI, system)

	provider, system = providerAndSystem(genaisdk.BackendGeminiAPI)
	assert.Equal(t, providerGemini, provider)
	assert.Equal(t, systemGemini, system)

	// An unset backend defaults to the Gemini Developer API, matching the SDK.
	provider, system = providerAndSystem(genaisdk.BackendUnspecified)
	assert.Equal(t, providerGemini, provider)
	assert.Equal(t, systemGemini, system)
}

func TestAfterNewClient_Enabled(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	httpClient := &http.Client{}
	client := newTestClient(t, genaisdk.BackendGeminiAPI, httpClient)

	AfterNewClient(hooktest.NewMockHookContext(), client, nil)

	transport, ok := httpClient.Transport.(*otelTransport)
	require.True(t, ok, "transport should be wrapped")
	assert.Equal(t, providerGemini, transport.provider)
	assert.Equal(t, systemGemini, transport.system)
	// The SDK left Transport nil, so the wrapper falls back to the default.
	assert.Nil(t, transport.base)

	// The client the SDK actually issues requests through is the same one.
	assert.Same(t, httpClient, client.ClientConfig().HTTPClient)
}

func TestAfterNewClient_VertexBackend(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	httpClient := &http.Client{}
	client := newTestClient(t, genaisdk.BackendVertexAI, httpClient)

	AfterNewClient(hooktest.NewMockHookContext(), client, nil)

	transport, ok := httpClient.Transport.(*otelTransport)
	require.True(t, ok)
	assert.Equal(t, providerVertexAI, transport.provider)
	assert.Equal(t, systemVertexAI, transport.system)
}

func TestAfterNewClient_PreservesExistingTransport(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
	})
	httpClient := &http.Client{Transport: base}
	client := newTestClient(t, genaisdk.BackendGeminiAPI, httpClient)

	AfterNewClient(hooktest.NewMockHookContext(), client, nil)

	transport, ok := httpClient.Transport.(*otelTransport)
	require.True(t, ok)
	assert.NotNil(t, transport.base, "the caller's transport must stay in the chain")
}

func TestAfterNewClient_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "GEMINI")

	httpClient := &http.Client{}
	client := newTestClient(t, genaisdk.BackendGeminiAPI, httpClient)

	AfterNewClient(hooktest.NewMockHookContext(), client, nil)

	assert.Nil(t, httpClient.Transport, "a disabled instrumentation must not touch the client")
}

func TestAfterNewClient_ConstructorFailure(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	// NewClient returning an error leaves nothing to instrument.
	assert.NotPanics(t, func() {
		AfterNewClient(hooktest.NewMockHookContext(), nil, errors.New("bad config"))
	})

	httpClient := &http.Client{}
	client := newTestClient(t, genaisdk.BackendGeminiAPI, httpClient)
	AfterNewClient(hooktest.NewMockHookContext(), client, errors.New("bad config"))
	assert.Nil(t, httpClient.Transport)
}

func TestAfterNewClient_DoesNotDoubleWrap(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	// Two clients sharing one *http.Client must yield one span per request,
	// not one per client.
	httpClient := &http.Client{}
	first := newTestClient(t, genaisdk.BackendGeminiAPI, httpClient)
	second := newTestClient(t, genaisdk.BackendGeminiAPI, httpClient)

	AfterNewClient(hooktest.NewMockHookContext(), first, nil)
	wrapped := httpClient.Transport

	AfterNewClient(hooktest.NewMockHookContext(), second, nil)

	assert.Same(t, wrapped, httpClient.Transport, "transport must be wrapped exactly once")
}
