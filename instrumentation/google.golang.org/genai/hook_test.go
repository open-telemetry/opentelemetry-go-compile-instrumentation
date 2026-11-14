// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package genai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	genaisdk "google.golang.org/genai"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

// newTestClient builds a Gemini API client without reaching the network, running
// the hooks the way the compiled rule does: BeforeNewClient on the config, then
// NewClient, then AfterNewClient on its results. A base URL suppresses endpoint
// resolution and an API key keeps NewClient off the
// application-default-credentials path.
func newTestClient(t *testing.T, backend genaisdk.Backend, httpClient *http.Client) *genaisdk.Client {
	t.Helper()

	return newInstrumentedClient(t, testClientConfig(backend, httpClient))
}

// testClientConfig returns the config newTestClient uses.
func testClientConfig(backend genaisdk.Backend, httpClient *http.Client) *genaisdk.ClientConfig {
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

	return config
}

func newInstrumentedClient(t *testing.T, config *genaisdk.ClientConfig) *genaisdk.Client {
	t.Helper()

	client, err := buildInstrumentedClient(t.Context(), config)
	require.NoError(t, err)

	return client
}

// buildInstrumentedClient runs the hooks around genai.NewClient. It takes no
// *testing.T, so it is safe to call from worker goroutines.
func buildInstrumentedClient(ctx context.Context, config *genaisdk.ClientConfig) (*genaisdk.Client, error) {
	BeforeNewClient(hooktest.NewMockHookContext(), ctx, config)
	client, err := genaisdk.NewClient(ctx, config)
	if err != nil {
		return nil, err
	}
	AfterNewClient(hooktest.NewMockHookContext(), client, nil)

	return client, nil
}

// sdkTransport returns the transport the SDK will issue requests through.
func sdkTransport(client *genaisdk.Client) http.RoundTripper {
	return client.ClientConfig().HTTPClient.Transport
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

func TestNewClient_Enabled(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	httpClient := &http.Client{}
	client := newTestClient(t, genaisdk.BackendGeminiAPI, httpClient)

	transport, ok := sdkTransport(client).(*otelTransport)
	require.True(t, ok, "transport should be wrapped")
	assert.Equal(t, providerGemini, transport.provider)
	assert.Equal(t, systemGemini, transport.system)
	// The SDK left Transport nil, so the wrapper falls back to the default.
	assert.Nil(t, transport.base)

	// The caller's client is left exactly as it was handed in.
	assert.NotSame(t, httpClient, client.ClientConfig().HTTPClient)
	assert.Nil(t, httpClient.Transport, "the caller's *http.Client must not be written to")
}

func TestNewClient_SDKBuiltClientIsWrapped(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	// No HTTPClient: NewClient builds one and AfterNewClient wraps it.
	client := newTestClient(t, genaisdk.BackendGeminiAPI, nil)

	_, ok := sdkTransport(client).(*otelTransport)
	assert.True(t, ok)
}

func TestNewClient_VertexBackend(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	client := newTestClient(t, genaisdk.BackendVertexAI, &http.Client{})

	transport, ok := sdkTransport(client).(*otelTransport)
	require.True(t, ok)
	assert.Equal(t, providerVertexAI, transport.provider)
	assert.Equal(t, systemVertexAI, transport.system)
}

func TestNewClient_PreservesExistingTransport(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
	})
	httpClient := &http.Client{Transport: base}
	client := newTestClient(t, genaisdk.BackendGeminiAPI, httpClient)

	transport, ok := sdkTransport(client).(*otelTransport)
	require.True(t, ok)
	assert.NotNil(t, transport.base, "the caller's transport must stay in the chain")
	_, stillPlain := httpClient.Transport.(roundTripFunc)
	assert.True(t, stillPlain, "the caller's transport must not be replaced")
}

func TestNewClient_Disabled(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "GEMINI")

	httpClient := &http.Client{}
	client := newTestClient(t, genaisdk.BackendGeminiAPI, httpClient)

	assert.Same(t, httpClient, client.ClientConfig().HTTPClient, "a disabled instrumentation must not copy the client")
	assert.Nil(t, httpClient.Transport, "a disabled instrumentation must not touch the client")
}

func TestAfterNewClient_ConstructorFailure(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	// NewClient returning an error leaves nothing to instrument.
	assert.NotPanics(t, func() {
		AfterNewClient(hooktest.NewMockHookContext(), nil, errors.New("bad config"))
	})

	httpClient := &http.Client{}
	client, err := genaisdk.NewClient(t.Context(), &genaisdk.ClientConfig{
		APIKey:      "test-key",
		Backend:     genaisdk.BackendGeminiAPI,
		HTTPClient:  httpClient,
		HTTPOptions: genaisdk.HTTPOptions{BaseURL: "http://127.0.0.1:0"},
	})
	require.NoError(t, err)
	AfterNewClient(hooktest.NewMockHookContext(), client, errors.New("bad config"))
	assert.Nil(t, httpClient.Transport)
}

func TestNewClient_SharedHTTPClientGetsOneWrapperPerClient(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	// Two genai clients built over one *http.Client each get their own wrapped
	// copy, so a request passes through exactly one otelTransport and each
	// client reports its own backend.
	httpClient := &http.Client{}
	gemini := newTestClient(t, genaisdk.BackendGeminiAPI, httpClient)
	vertex := newTestClient(t, genaisdk.BackendVertexAI, httpClient)

	geminiTransport, ok := sdkTransport(gemini).(*otelTransport)
	require.True(t, ok)
	vertexTransport, ok := sdkTransport(vertex).(*otelTransport)
	require.True(t, ok)

	assert.Nil(t, geminiTransport.base, "wrappers must not nest")
	assert.Nil(t, vertexTransport.base, "wrappers must not nest")
	assert.Equal(t, providerGemini, geminiTransport.provider)
	assert.Equal(t, providerVertexAI, vertexTransport.provider)
	assert.Nil(t, httpClient.Transport)
}

func TestNewClient_ReusedConfigDoesNotDoubleWrap(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	config := &genaisdk.ClientConfig{
		APIKey:      "test-key",
		Backend:     genaisdk.BackendGeminiAPI,
		HTTPClient:  &http.Client{},
		HTTPOptions: genaisdk.HTTPOptions{BaseURL: "http://127.0.0.1:0"},
	}
	first := newInstrumentedClient(t, config)
	second := newInstrumentedClient(t, config)

	assert.Same(t, sdkTransport(first), sdkTransport(second), "transport must be wrapped exactly once")
	transport, ok := sdkTransport(second).(*otelTransport)
	require.True(t, ok)
	assert.Nil(t, transport.base)
}

func TestNewClient_ConcurrentUseOfCallerClientIsRaceFree(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "GEMINI")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	// The caller keeps using its client while genai clients are built over
	// it. Run with -race: writing httpClient.Transport here would be flagged.
	httpClient := &http.Client{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				resp, err := httpClient.Get(server.URL)
				if assert.NoError(t, err) {
					_ = resp.Body.Close()
				}
			}
		}()
	}
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := buildInstrumentedClient(
				t.Context(), testClientConfig(genaisdk.BackendGeminiAPI, httpClient))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	assert.Nil(t, httpClient.Transport)
}
