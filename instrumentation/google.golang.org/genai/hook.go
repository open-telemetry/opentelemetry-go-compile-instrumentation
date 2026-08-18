// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package genai

import (
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	genaisdk "google.golang.org/genai"

	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/google.golang.org/genai"
	instrumentationKey  = "GEMINI"
)

var (
	logger   = runtime.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

type geminiEnabler struct{}

func (geminiEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = geminiEnabler{}

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)

		logger.Info("Gemini instrumentation initialized")
	})
}

// Values for gen_ai.provider.name and gen_ai.system, selected by the backend
// the client was configured for. The GenAI semantic conventions give Google two
// distinct providers: the Gemini Developer API and Vertex AI.
const (
	providerGemini   = "gcp.gemini"
	providerVertexAI = "gcp.vertex_ai"

	systemGemini   = "gemini"
	systemVertexAI = "vertex_ai"
)

// providerAndSystem maps the SDK backend onto the gen_ai.provider.name and
// gen_ai.system values. Reading the backend off the client is exact, unlike the
// request-host keyword matching the openai-go and anthropic-sdk-go
// instrumentations fall back on, because the SDK records which API it targets.
func providerAndSystem(backend genaisdk.Backend) (provider, system string) {
	if backend == genaisdk.BackendVertexAI {
		return providerVertexAI, systemVertexAI
	}
	return providerGemini, systemGemini
}

// AfterNewClient matches the return values of genai.NewClient and installs the
// tracing transport on the HTTP client the SDK will use.
//
// The wrapping happens after the fact, rather than in a Before hook on the
// *genai.ClientConfig argument, because NewClient itself fills in a nil
// HTTPClient -- and for Vertex AI without explicit credentials, the client it
// builds carries application-default-credentials authentication. Replacing that
// client up front would break authentication, so instead we decorate the
// transport of whatever client NewClient settled on.
//
// Client.ClientConfig returns a copy of the config, but HTTPClient is a pointer
// that the copy shares with the internal apiClient issuing the requests, so
// swapping the transport through it takes effect for every subsequent call.
func AfterNewClient(ictx hook.HookContext, client *genaisdk.Client, err error) {
	if !enabler.Enable() {
		return
	}
	if err != nil || client == nil {
		return
	}
	initInstrumentation()

	config := client.ClientConfig()
	httpClient := config.HTTPClient
	if httpClient == nil {
		// NewClient always populates HTTPClient on success, so this only
		// happens if that contract changes upstream. Skip rather than
		// substitute a client the SDK did not build.
		logger.Debug("Gemini client has no HTTP client; skipping instrumentation")
		return
	}
	if _, ok := httpClient.Transport.(*otelTransport); ok {
		// Callers may build several clients over one *http.Client; wrapping it
		// again would emit a duplicate span per request.
		return
	}

	provider, system := providerAndSystem(config.Backend)
	httpClient.Transport = &otelTransport{
		base:     httpClient.Transport,
		provider: provider,
		system:   system,
	}
}
