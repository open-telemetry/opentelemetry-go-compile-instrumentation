// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func makeParentContext(remote, sampled bool, ts trace.TraceState) context.Context {
	tid, _ := trace.TraceIDFromHex("12345678901234567890123456789012")
	sid, _ := trace.SpanIDFromHex("1234567890123456")
	var flags trace.TraceFlags
	if sampled {
		flags |= trace.FlagsSampled
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: flags,
		TraceState: ts,
		Remote:     remote,
	})
	return trace.ContextWithSpanContext(context.Background(), sc)
}

func TestParseExcludedPaths(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected map[string]struct{}
	}{
		{
			name:     "empty string",
			raw:      "",
			expected: nil,
		},
		{
			name:     "whitespace only",
			raw:      "   \t  ",
			expected: nil,
		},
		{
			name:     "commas only",
			raw:      ",,,",
			expected: nil,
		},
		{
			name:     "single path",
			raw:      "/healthz",
			expected: map[string]struct{}{"/healthz": {}},
		},
		{
			name: "multiple paths with spaces",
			raw:  " /healthz , /readyz , /livez ",
			expected: map[string]struct{}{
				"/healthz": {},
				"/readyz":  {},
				"/livez":   {},
			},
		},
		{
			name: "paths with empty entries",
			raw:  ",/healthz,,/readyz,",
			expected: map[string]struct{}{
				"/healthz": {},
				"/readyz":  {},
			},
		},
		{
			name: "case sensitive paths preserved",
			raw:  "/Healthz,/READY",
			expected: map[string]struct{}{
				"/Healthz": {},
				"/READY":   {},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseExcludedPaths(tc.raw)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestParseDropRootClientProducer(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{" true ", true},
		{"false", false},
		{"FALSE", false},
		{"1", false},
		{"yes", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			assert.Equal(t, tc.want, parseDropRootClientProducer(tc.raw))
		})
	}
}

func TestRootFilteringSampler_RootFiltering(t *testing.T) {
	tests := []struct {
		name                   string
		dropRootClientProducer bool
		excludedPaths          map[string]struct{}
		parentCtx              context.Context
		kind                   trace.SpanKind
		attrs                  []attribute.KeyValue
		wantDecision           sdktrace.SamplingDecision
	}{
		{
			name:                   "parentless CLIENT dropped when enabled",
			dropRootClientProducer: true,
			parentCtx:              context.Background(),
			kind:                   trace.SpanKindClient,
			wantDecision:           sdktrace.Drop,
		},
		{
			name:                   "parentless PRODUCER dropped when enabled",
			dropRootClientProducer: true,
			parentCtx:              context.Background(),
			kind:                   trace.SpanKindProducer,
			wantDecision:           sdktrace.Drop,
		},
		{
			name:                   "parentless CLIENT sampled when disabled",
			dropRootClientProducer: false,
			parentCtx:              context.Background(),
			kind:                   trace.SpanKindClient,
			wantDecision:           sdktrace.RecordAndSample,
		},
		{
			name:                   "parentless PRODUCER sampled when disabled",
			dropRootClientProducer: false,
			parentCtx:              context.Background(),
			kind:                   trace.SpanKindProducer,
			wantDecision:           sdktrace.RecordAndSample,
		},
		{
			name:                   "root INTERNAL unaffected",
			dropRootClientProducer: true,
			parentCtx:              context.Background(),
			kind:                   trace.SpanKindInternal,
			wantDecision:           sdktrace.RecordAndSample,
		},
		{
			name:                   "root CONSUMER unaffected",
			dropRootClientProducer: true,
			parentCtx:              context.Background(),
			kind:                   trace.SpanKindConsumer,
			wantDecision:           sdktrace.RecordAndSample,
		},
		{
			name:                   "root SERVER unaffected without excluded path",
			dropRootClientProducer: true,
			excludedPaths:          map[string]struct{}{"/healthz": {}},
			parentCtx:              context.Background(),
			kind:                   trace.SpanKindServer,
			attrs:                  []attribute.KeyValue{attribute.String("url.path", "/api/data")},
			wantDecision:           sdktrace.RecordAndSample,
		},
		{
			name:                   "root SERVER unaffected with no path attribute",
			dropRootClientProducer: true,
			excludedPaths:          map[string]struct{}{"/healthz": {}},
			parentCtx:              context.Background(),
			kind:                   trace.SpanKindServer,
			attrs:                  nil,
			wantDecision:           sdktrace.RecordAndSample,
		},
		{
			name:                   "CLIENT under valid sampled parent delegated normally",
			dropRootClientProducer: true,
			parentCtx:              makeParentContext(false, true, trace.TraceState{}),
			kind:                   trace.SpanKindClient,
			wantDecision:           sdktrace.RecordAndSample,
		},
		{
			name:                   "PRODUCER under valid sampled parent delegated normally",
			dropRootClientProducer: true,
			parentCtx:              makeParentContext(false, true, trace.TraceState{}),
			kind:                   trace.SpanKindProducer,
			wantDecision:           sdktrace.RecordAndSample,
		},
		{
			name:                   "CLIENT under valid remote parent delegated normally",
			dropRootClientProducer: true,
			parentCtx:              makeParentContext(true, true, trace.TraceState{}),
			kind:                   trace.SpanKindClient,
			wantDecision:           sdktrace.RecordAndSample,
		},
		{
			name:                   "both filtering options disabled delegates all",
			dropRootClientProducer: false,
			excludedPaths:          nil,
			parentCtx:              context.Background(),
			kind:                   trace.SpanKindClient,
			wantDecision:           sdktrace.RecordAndSample,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sampler := newRootFilteringSampler(sdktrace.AlwaysSample(), tc.dropRootClientProducer, tc.excludedPaths)
			res := sampler.ShouldSample(sdktrace.SamplingParameters{
				ParentContext: tc.parentCtx,
				Kind:          tc.kind,
				Attributes:    tc.attrs,
			})
			assert.Equal(t, tc.wantDecision, res.Decision)
		})
	}
}

func TestRootFilteringSampler_HTTPPaths(t *testing.T) {
	excluded := map[string]struct{}{
		"/healthz": {},
		"/readyz":  {},
	}

	tests := []struct {
		name         string
		path         string
		query        string
		remoteParent bool
		wantDecision sdktrace.SamplingDecision
	}{
		{
			name:         "/healthz excluded",
			path:         "/healthz",
			wantDecision: sdktrace.Drop,
		},
		{
			name:         "/readyz excluded",
			path:         "/readyz",
			wantDecision: sdktrace.Drop,
		},
		{
			name:         "/healthz/details not excluded",
			path:         "/healthz/details",
			wantDecision: sdktrace.RecordAndSample,
		},
		{
			name:         "/healthz/ trailing slash not excluded",
			path:         "/healthz/",
			wantDecision: sdktrace.RecordAndSample,
		},
		{
			name:         "/Healthz case-sensitive not excluded",
			path:         "/Healthz",
			wantDecision: sdktrace.RecordAndSample,
		},
		{
			name:         "query string does not affect path matching",
			path:         "/healthz",
			query:        "verbose=true&check=live",
			wantDecision: sdktrace.Drop,
		},
		{
			name:         "remote-parent SERVER spans are delegated (not dropped)",
			path:         "/healthz",
			remoteParent: true,
			wantDecision: sdktrace.RecordAndSample,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sampler := newRootFilteringSampler(sdktrace.AlwaysSample(), false, excluded)
			var parentCtx context.Context
			if tc.remoteParent {
				parentCtx = makeParentContext(true, true, trace.TraceState{})
			} else {
				parentCtx = context.Background()
			}

			attrs := []attribute.KeyValue{
				attribute.String("url.path", tc.path),
			}
			if tc.query != "" {
				attrs = append(attrs, attribute.String("url.query", tc.query))
			}

			res := sampler.ShouldSample(sdktrace.SamplingParameters{
				ParentContext: parentCtx,
				Kind:          trace.SpanKindServer,
				Attributes:    attrs,
			})
			assert.Equal(t, tc.wantDecision, res.Decision)
		})
	}
}

func TestRootFilteringSampler_DescendantAndTraceState(t *testing.T) {
	ts, err := trace.ParseTraceState("vendor=opaque123,app=test")
	require.NoError(t, err)

	sampler := newRootFilteringSampler(sdktrace.AlwaysSample(), true, map[string]struct{}{"/healthz": {}})

	t.Run("filtered root preserves tracestate", func(t *testing.T) {
		res := sampler.ShouldSample(sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			Kind:          trace.SpanKindClient,
		})
		assert.Equal(t, sdktrace.Drop, res.Decision)
		assert.Equal(t, "", res.Tracestate.String())
	})

	t.Run("local child of unsampled parent is suppressed and preserves parent tracestate", func(t *testing.T) {
		parentCtx := makeParentContext(false, false, ts)
		res := sampler.ShouldSample(sdktrace.SamplingParameters{
			ParentContext: parentCtx,
			Kind:          trace.SpanKindInternal,
		})
		assert.Equal(t, sdktrace.Drop, res.Decision)
		assert.Equal(t, ts, res.Tracestate)
	})

	t.Run("remote child of unsampled parent delegates to underlying sampler", func(t *testing.T) {
		// With underlying AlwaysSample, remote unsampled parent is sampled if delegated
		parentCtx := makeParentContext(true, false, ts)
		res := sampler.ShouldSample(sdktrace.SamplingParameters{
			ParentContext: parentCtx,
			Kind:          trace.SpanKindServer,
		})
		// AlwaysSample delegates and returns RecordAndSample
		assert.Equal(t, sdktrace.RecordAndSample, res.Decision)
	})
}

func TestRootFilteringSampler_Description(t *testing.T) {
	paths := map[string]struct{}{
		"/readyz":  {},
		"/healthz": {},
	}
	s := newRootFilteringSampler(sdktrace.AlwaysSample(), true, paths)
	assert.Equal(t,
		"RootFilteringSampler{dropRootClientProducer:true,excludedPaths:[/healthz,/readyz],delegate:AlwaysOnSampler}",
		s.Description(),
	)
}

func TestSDKSamplerFromEnv_Parity(t *testing.T) {
	testCases := []struct {
		name       string
		samplerEnv string
		argEnv     string
		desc       string
	}{
		{
			name:       "default unset",
			samplerEnv: "",
			argEnv:     "",
			desc:       sdktrace.ParentBased(sdktrace.AlwaysSample()).Description(),
		},
		{
			name:       "always_on",
			samplerEnv: "always_on",
			argEnv:     "",
			desc:       sdktrace.AlwaysSample().Description(),
		},
		{
			name:       "always_off",
			samplerEnv: "always_off",
			argEnv:     "",
			desc:       sdktrace.NeverSample().Description(),
		},
		{
			name:       "traceidratio default arg",
			samplerEnv: "traceidratio",
			argEnv:     "",
			desc:       sdktrace.TraceIDRatioBased(1.0).Description(),
		},
		{
			name:       "traceidratio 0.5",
			samplerEnv: "traceidratio",
			argEnv:     "0.5",
			desc:       sdktrace.TraceIDRatioBased(0.5).Description(),
		},
		{
			name:       "traceidratio 0.0",
			samplerEnv: "traceidratio",
			argEnv:     "0.0",
			desc:       sdktrace.TraceIDRatioBased(0.0).Description(),
		},
		{
			name:       "traceidratio 1.0",
			samplerEnv: "traceidratio",
			argEnv:     "1.0",
			desc:       sdktrace.TraceIDRatioBased(1.0).Description(),
		},
		{
			name:       "traceidratio invalid negative fallback",
			samplerEnv: "traceidratio",
			argEnv:     "-0.1",
			desc:       sdktrace.TraceIDRatioBased(1.0).Description(),
		},
		{
			name:       "traceidratio invalid greater than 1 fallback",
			samplerEnv: "traceidratio",
			argEnv:     "1.5",
			desc:       sdktrace.TraceIDRatioBased(1.0).Description(),
		},
		{
			name:       "traceidratio invalid string fallback",
			samplerEnv: "traceidratio",
			argEnv:     "not-a-number",
			desc:       sdktrace.TraceIDRatioBased(1.0).Description(),
		},
		{
			name:       "parentbased_always_on",
			samplerEnv: "parentbased_always_on",
			argEnv:     "",
			desc:       sdktrace.ParentBased(sdktrace.AlwaysSample()).Description(),
		},
		{
			name:       "parentbased_always_off",
			samplerEnv: "parentbased_always_off",
			argEnv:     "",
			desc:       sdktrace.ParentBased(sdktrace.NeverSample()).Description(),
		},
		{
			name:       "parentbased_traceidratio default arg",
			samplerEnv: "parentbased_traceidratio",
			argEnv:     "",
			desc:       sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0)).Description(),
		},
		{
			name:       "parentbased_traceidratio 0.5",
			samplerEnv: "parentbased_traceidratio",
			argEnv:     "0.5",
			desc:       sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.5)).Description(),
		},
		{
			name:       "parentbased_traceidratio invalid arg fallback",
			samplerEnv: "parentbased_traceidratio",
			argEnv:     "-0.5",
			desc:       sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0)).Description(),
		},
		{
			name:       "unsupported sampler fallback",
			samplerEnv: "unknown_sampler_xyz",
			argEnv:     "",
			desc:       sdktrace.ParentBased(sdktrace.AlwaysSample()).Description(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tracesSamplerKey, tc.samplerEnv)
			t.Setenv(tracesSamplerArgKey, tc.argEnv)

			sampler := sdkSamplerFromEnv()
			assert.Equal(t, tc.desc, sampler.Description())

			// Also verify that a TracerProvider created without options (SDK's own env resolution)
			// behaves identically for shouldSample on standard contexts.
			sdkTP := sdktrace.NewTracerProvider()
			customTP := sdktrace.NewTracerProvider(sdktrace.WithSampler(sampler))

			// Test root span for deterministic samplers
			if tc.samplerEnv != "traceidratio" && tc.samplerEnv != "parentbased_traceidratio" || tc.argEnv == "" ||
				tc.argEnv == "0.0" ||
				tc.argEnv == "1.0" ||
				tc.argEnv == "-0.1" ||
				tc.argEnv == "1.5" ||
				tc.argEnv == "not-a-number" ||
				tc.argEnv == "-0.5" {
				_, sdkSpan := sdkTP.Tracer("test").Start(context.Background(), "test-span")
				_, customSpan := customTP.Tracer("test").Start(context.Background(), "test-span")
				assert.Equal(t, sdkSpan.SpanContext().IsSampled(), customSpan.SpanContext().IsSampled())
			}

			// Test remote sampled parent
			remoteSampled := makeParentContext(true, true, trace.TraceState{})
			_, sdkSpanRemote := sdkTP.Tracer("test").Start(remoteSampled, "test-span-remote")
			_, customSpanRemote := customTP.Tracer("test").Start(remoteSampled, "test-span-remote")
			assert.Equal(t, sdkSpanRemote.SpanContext().IsSampled(), customSpanRemote.SpanContext().IsSampled())

			// Test remote unsampled parent
			remoteUnsampled := makeParentContext(true, false, trace.TraceState{})
			_, sdkSpanRemoteUn := sdkTP.Tracer("test").Start(remoteUnsampled, "test-span-remote-un")
			_, customSpanRemoteUn := customTP.Tracer("test").Start(remoteUnsampled, "test-span-remote-un")
			assert.Equal(t, sdkSpanRemoteUn.SpanContext().IsSampled(), customSpanRemoteUn.SpanContext().IsSampled())
		})
	}
}

func TestRealSDKTracer_DescendantSuppression(t *testing.T) {
	// Tests with real SDK tracer and in-memory exporter:
	// Verify that filtered roots become non-recording spans and all their local descendants
	// (multiple levels) are suppressed even under non-parent-based samplers like always_on.
	exporter := tracetest.NewInMemoryExporter()
	delegate := sdktrace.AlwaysSample()
	filteringSampler := newRootFilteringSampler(delegate, true, map[string]struct{}{"/healthz": {}})

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(filteringSampler),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	tracer := tp.Tracer("test-tracer")

	t.Run("parentless client root and descendants are suppressed", func(t *testing.T) {
		exporter.Reset()

		ctx, clientSpan := tracer.Start(context.Background(), "db-query", trace.WithSpanKind(trace.SpanKindClient))
		assert.False(t, clientSpan.IsRecording(), "filtered root span must be non-recording")
		assert.False(t, clientSpan.SpanContext().IsSampled(), "filtered root span must not be sampled")

		// Child span
		ctx2, childSpan := tracer.Start(ctx, "child-op", trace.WithSpanKind(trace.SpanKindInternal))
		assert.False(t, childSpan.IsRecording(), "child span must be non-recording")
		assert.False(t, childSpan.SpanContext().IsSampled(), "child span must not be sampled")

		// Grandchild span
		_, grandchildSpan := tracer.Start(ctx2, "grandchild-op", trace.WithSpanKind(trace.SpanKindInternal))
		assert.False(t, grandchildSpan.IsRecording(), "grandchild span must be non-recording")
		assert.False(t, grandchildSpan.SpanContext().IsSampled(), "grandchild span must not be sampled")

		grandchildSpan.End()
		childSpan.End()
		clientSpan.End()

		assert.Empty(t, exporter.GetSpans(), "no spans should be exported for filtered client root trace")
	})

	t.Run("excluded server root and descendants are suppressed", func(t *testing.T) {
		exporter.Reset()

		ctx, serverSpan := tracer.Start(context.Background(), "GET /healthz",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/healthz")),
		)
		assert.False(t, serverSpan.IsRecording(), "filtered server root span must be non-recording")

		// Child DB client span inside /healthz handler
		ctx2, dbSpan := tracer.Start(ctx, "SELECT 1", trace.WithSpanKind(trace.SpanKindClient))
		assert.False(t, dbSpan.IsRecording(), "descendant client span inside /healthz must be non-recording")

		// Grandchild span inside DB query
		_, subSpan := tracer.Start(ctx2, "db-internal", trace.WithSpanKind(trace.SpanKindInternal))
		assert.False(t, subSpan.IsRecording(), "grandchild span inside /healthz must be non-recording")

		subSpan.End()
		dbSpan.End()
		serverSpan.End()

		assert.Empty(t, exporter.GetSpans(), "no spans should be exported for excluded server root trace")
	})

	t.Run("normal sampled server trace exports root and descendants", func(t *testing.T) {
		exporter.Reset()

		ctx, serverSpan := tracer.Start(context.Background(), "GET /users",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/users")),
		)
		assert.True(t, serverSpan.IsRecording(), "normal server root span must be recording")
		assert.True(t, serverSpan.SpanContext().IsSampled(), "normal server root span must be sampled")

		// Child DB client span inside /users handler
		_, dbSpan := tracer.Start(ctx, "SELECT * FROM users", trace.WithSpanKind(trace.SpanKindClient))
		assert.True(t, dbSpan.IsRecording(), "child db span must be recording")
		assert.True(t, dbSpan.SpanContext().IsSampled(), "child db span must be sampled")

		dbSpan.End()
		serverSpan.End()

		spans := exporter.GetSpans()
		require.Len(t, spans, 2, "normal trace should export both server and child db spans")
		assert.Equal(t, "SELECT * FROM users", spans[0].Name)
		assert.Equal(t, "GET /users", spans[1].Name)
		assert.Equal(t, spans[1].SpanContext.SpanID(), spans[0].Parent.SpanID())
	})
}

func TestNewFilteringSamplerFromEnv(t *testing.T) {
	t.Run("both disabled returns nil", func(t *testing.T) {
		t.Setenv(envDropRootClientProducer, "")
		t.Setenv(envExcludedServerPaths, "")
		assert.Nil(t, newFilteringSamplerFromEnv())
	})

	t.Run("drop client producer enabled returns sampler", func(t *testing.T) {
		t.Setenv(envDropRootClientProducer, "true")
		t.Setenv(envExcludedServerPaths, "")
		s := newFilteringSamplerFromEnv()
		require.NotNil(t, s)
		assert.Contains(t, s.Description(), "dropRootClientProducer:true")
	})

	t.Run("excluded paths enabled returns sampler", func(t *testing.T) {
		t.Setenv(envDropRootClientProducer, "false")
		t.Setenv(envExcludedServerPaths, "/healthz,/readyz")
		s := newFilteringSamplerFromEnv()
		require.NotNil(t, s)
		assert.Contains(t, s.Description(), "excludedPaths:[/healthz,/readyz]")
	})
}

func TestSetupTraceProviderWithFilteringSampler(t *testing.T) {
	restoreProviders(t)
	tracerProvider = nil

	t.Setenv("OTEL_TRACES_EXPORTER", "console")
	t.Setenv(envDropRootClientProducer, "true")
	t.Setenv(envExcludedServerPaths, "/healthz")

	err := setupTraceProvider(context.Background(), resource.Default())
	require.NoError(t, err)
	require.NotNil(t, tracerProvider)

	// Tracer from initialized provider should drop root CLIENT
	tr := otel.GetTracerProvider().Tracer("test")
	_, span := tr.Start(context.Background(), "client-op", trace.WithSpanKind(trace.SpanKindClient))
	assert.False(t, span.IsRecording(), "span should be non-recording under filtering sampler")
}

func TestSetupTraceProvider_NoErrorDuplication(t *testing.T) {
	restoreProviders(t)
	tracerProvider = nil

	var errorCount int
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		errorCount++
	}))

	t.Setenv("OTEL_TRACES_EXPORTER", "console")
	t.Setenv(envDropRootClientProducer, "true")
	t.Setenv(tracesSamplerKey, "invalid_sampler_choice")

	err := setupTraceProvider(context.Background(), resource.Default())
	require.NoError(t, err)
	assert.Equal(t, 1, errorCount, "SDK error handler should be called exactly once for invalid sampler")
}

func TestAcceptanceScenarios(t *testing.T) {
	// 1. Filtering disabled with always_on preserves existing behavior.
	t.Run("Scenario 1: Filtering disabled with always_on preserves existing behavior", func(t *testing.T) {
		t.Setenv(envDropRootClientProducer, "")
		t.Setenv(envExcludedServerPaths, "")
		t.Setenv(tracesSamplerKey, "always_on")

		assert.Nil(t, newFilteringSamplerFromEnv())

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		)
		tr := tp.Tracer("acceptance-tracer")

		_, clientSpan := tr.Start(context.Background(), "client-root", trace.WithSpanKind(trace.SpanKindClient))
		assert.True(t, clientSpan.IsRecording())
		assert.True(t, clientSpan.SpanContext().IsSampled())
		clientSpan.End()

		_, producerSpan := tr.Start(context.Background(), "producer-root", trace.WithSpanKind(trace.SpanKindProducer))
		assert.True(t, producerSpan.IsRecording())
		assert.True(t, producerSpan.SpanContext().IsSampled())
		producerSpan.End()

		_, serverSpan := tr.Start(context.Background(), "server-healthz",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/healthz")),
		)
		assert.True(t, serverSpan.IsRecording())
		assert.True(t, serverSpan.SpanContext().IsSampled())
		serverSpan.End()

		assert.Len(t, exporter.GetSpans(), 3)
	})

	// 2. Filtering disabled with always_off preserves existing behavior.
	t.Run("Scenario 2: Filtering disabled with always_off preserves existing behavior", func(t *testing.T) {
		t.Setenv(envDropRootClientProducer, "")
		t.Setenv(envExcludedServerPaths, "")
		t.Setenv(tracesSamplerKey, "always_off")

		assert.Nil(t, newFilteringSamplerFromEnv())

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		)
		tr := tp.Tracer("acceptance-tracer")

		_, clientSpan := tr.Start(context.Background(), "client-root", trace.WithSpanKind(trace.SpanKindClient))
		assert.False(t, clientSpan.IsRecording())
		assert.False(t, clientSpan.SpanContext().IsSampled())
		clientSpan.End()

		_, serverSpan := tr.Start(context.Background(), "server-api",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/api/data")),
		)
		assert.False(t, serverSpan.IsRecording())
		assert.False(t, serverSpan.SpanContext().IsSampled())
		serverSpan.End()

		assert.Empty(t, exporter.GetSpans())
	})

	// 3. Root CLIENT and PRODUCER suppression works independently.
	t.Run("Scenario 3: Root CLIENT and PRODUCER suppression works independently", func(t *testing.T) {
		// 3a: Only dropRootClientProducer is enabled
		samplerClientOnly := newRootFilteringSampler(sdktrace.AlwaysSample(), true, nil)
		exporterA := tracetest.NewInMemoryExporter()
		tpA := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(samplerClientOnly),
			sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporterA)),
		)
		trA := tpA.Tracer("tr-a")

		// Root CLIENT and PRODUCER dropped
		_, cSpan := trA.Start(context.Background(), "client-root", trace.WithSpanKind(trace.SpanKindClient))
		assert.False(t, cSpan.IsRecording())
		cSpan.End()

		_, pSpan := trA.Start(context.Background(), "producer-root", trace.WithSpanKind(trace.SpanKindProducer))
		assert.False(t, pSpan.IsRecording())
		pSpan.End()

		// Root SERVER on /healthz is NOT dropped
		_, sSpan := trA.Start(context.Background(), "GET /healthz",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/healthz")),
		)
		assert.True(t, sSpan.IsRecording())
		sSpan.End()

		// Child CLIENT under sampled parent is NOT dropped
		sampledCtx := makeParentContext(false, true, trace.TraceState{})
		_, childClient := trA.Start(sampledCtx, "client-child", trace.WithSpanKind(trace.SpanKindClient))
		assert.True(t, childClient.IsRecording())
		childClient.End()

		spansA := exporterA.GetSpans()
		require.Len(t, spansA, 2)
		assert.Equal(t, "GET /healthz", spansA[0].Name)
		assert.Equal(t, "client-child", spansA[1].Name)

		// 3b: Only excludedPaths is enabled
		samplerPathOnly := newRootFilteringSampler(sdktrace.AlwaysSample(), false, map[string]struct{}{"/healthz": {}})
		exporterB := tracetest.NewInMemoryExporter()
		tpB := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(samplerPathOnly),
			sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporterB)),
		)
		trB := tpB.Tracer("tr-b")

		// Root CLIENT and PRODUCER are NOT dropped
		_, cSpan2 := trB.Start(context.Background(), "client-root", trace.WithSpanKind(trace.SpanKindClient))
		assert.True(t, cSpan2.IsRecording())
		cSpan2.End()

		_, pSpan2 := trB.Start(context.Background(), "producer-root", trace.WithSpanKind(trace.SpanKindProducer))
		assert.True(t, pSpan2.IsRecording())
		pSpan2.End()

		// Root SERVER on /healthz IS dropped
		_, sSpan2 := trB.Start(context.Background(), "GET /healthz",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/healthz")),
		)
		assert.False(t, sSpan2.IsRecording())
		sSpan2.End()

		spansB := exporterB.GetSpans()
		require.Len(t, spansB, 2)
		assert.Equal(t, "client-root", spansB[0].Name)
		assert.Equal(t, "producer-root", spansB[1].Name)
	})

	// 4. Excluded SERVER paths are dropped.
	t.Run("Scenario 4: Excluded SERVER paths are dropped", func(t *testing.T) {
		sampler := newRootFilteringSampler(sdktrace.AlwaysSample(), false, map[string]struct{}{
			"/healthz": {},
			"/readyz":  {},
		})
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sampler),
			sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		)
		tr := tp.Tracer("tr-4")

		// Exact matches dropped
		_, s1 := tr.Start(
			context.Background(),
			"GET /healthz",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/healthz")),
		)
		assert.False(t, s1.IsRecording())
		s1.End()

		_, s2 := tr.Start(
			context.Background(),
			"GET /readyz",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/readyz")),
		)
		assert.False(t, s2.IsRecording())
		s2.End()

		// Query string on exact path is still dropped (url.path is matched)
		_, s3 := tr.Start(context.Background(), "GET /healthz?full=1", trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/healthz"), attribute.String("url.query", "full=1")))
		assert.False(t, s3.IsRecording())
		s3.End()

		// Trailing slash, sub-path, and case differences are NOT dropped
		_, s4 := tr.Start(
			context.Background(),
			"GET /healthz/",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/healthz/")),
		)
		assert.True(t, s4.IsRecording())
		s4.End()

		_, s5 := tr.Start(
			context.Background(),
			"GET /healthz/all",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/healthz/all")),
		)
		assert.True(t, s5.IsRecording())
		s5.End()

		_, s6 := tr.Start(
			context.Background(),
			"GET /Healthz",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/Healthz")),
		)
		assert.True(t, s6.IsRecording())
		s6.End()

		// Remote parent on excluded path is NOT dropped
		remoteCtx := makeParentContext(true, true, trace.TraceState{})
		_, s7 := tr.Start(
			remoteCtx,
			"GET /healthz",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/healthz")),
		)
		assert.True(t, s7.IsRecording())
		s7.End()

		assert.Len(t, exporter.GetSpans(), 4)
	})

	// 5. Normal sampled trace trees remain unaffected.
	t.Run("Scenario 5: Normal sampled trace trees remain unaffected", func(t *testing.T) {
		sampler := newRootFilteringSampler(sdktrace.AlwaysSample(), true, map[string]struct{}{"/healthz": {}})
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sampler),
			sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		)
		tr := tp.Tracer("tr-5")

		ctx, root := tr.Start(context.Background(), "GET /items",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/items")),
		)
		assert.True(t, root.IsRecording())

		ctx2, dbChild := tr.Start(ctx, "query-items", trace.WithSpanKind(trace.SpanKindClient))
		assert.True(t, dbChild.IsRecording())

		_, internalChild := tr.Start(ctx2, "process-items", trace.WithSpanKind(trace.SpanKindInternal))
		assert.True(t, internalChild.IsRecording())

		internalChild.End()
		dbChild.End()
		root.End()

		spans := exporter.GetSpans()
		require.Len(t, spans, 3)
		assert.Equal(t, "process-items", spans[0].Name)
		assert.Equal(t, "query-items", spans[1].Name)
		assert.Equal(t, "GET /items", spans[2].Name)
		assert.Equal(t, spans[2].SpanContext.SpanID(), spans[1].Parent.SpanID())
		assert.Equal(t, spans[1].SpanContext.SpanID(), spans[0].Parent.SpanID())
	})

	// 6. Local descendants of excluded roots remain unsampled under always_on.
	t.Run("Scenario 6: Local descendants of excluded roots remain unsampled under always_on", func(t *testing.T) {
		sampler := newRootFilteringSampler(sdktrace.AlwaysSample(), true, map[string]struct{}{"/healthz": {}})
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sampler),
			sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		)
		tr := tp.Tracer("tr-6")

		ctx, root := tr.Start(context.Background(), "GET /healthz",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("url.path", "/healthz")),
		)
		assert.False(t, root.IsRecording())
		assert.False(t, root.SpanContext().IsSampled())

		ctx2, child := tr.Start(ctx, "health-db-check", trace.WithSpanKind(trace.SpanKindClient))
		assert.False(t, child.IsRecording())
		assert.False(t, child.SpanContext().IsSampled())

		_, grandchild := tr.Start(ctx2, "health-db-ping", trace.WithSpanKind(trace.SpanKindInternal))
		assert.False(t, grandchild.IsRecording())
		assert.False(t, grandchild.SpanContext().IsSampled())

		grandchild.End()
		child.End()
		root.End()

		assert.Empty(t, exporter.GetSpans())
	})

	// 7. Unrelated local unsampled parents also suppress descendants when filtering is enabled.
	t.Run(
		"Scenario 7: Unrelated local unsampled parents also suppress descendants when filtering is enabled",
		func(t *testing.T) {
			sampler := newRootFilteringSampler(sdktrace.AlwaysSample(), true, map[string]struct{}{"/healthz": {}})
			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(
				sdktrace.WithSampler(sampler),
				sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
			)
			tr := tp.Tracer("tr-7")

			// Create a context with an unrelated local unsampled parent
			unrelatedLocalUnsampled := makeParentContext(false, false, trace.TraceState{})

			// When filtering sampler is active, this child must remain unsampled
			_, child := tr.Start(
				unrelatedLocalUnsampled,
				"child-of-unsampled-local",
				trace.WithSpanKind(trace.SpanKindInternal),
			)
			assert.False(t, child.IsRecording())
			assert.False(t, child.SpanContext().IsSampled())
			child.End()

			assert.Empty(t, exporter.GetSpans())
		},
	)

	// 8. Remote unsampled parents remain delegated under always_on.
	t.Run("Scenario 8: Remote unsampled parents remain delegated under always_on", func(t *testing.T) {
		sampler := newRootFilteringSampler(sdktrace.AlwaysSample(), true, map[string]struct{}{"/healthz": {}})
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sampler),
			sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		)
		tr := tp.Tracer("tr-8")

		remoteUnsampled := makeParentContext(true, false, trace.TraceState{})
		_, span := tr.Start(remoteUnsampled, "server-remote-parent", trace.WithSpanKind(trace.SpanKindServer))
		// Under AlwaysSample delegate, remote unsampled parent is delegated and sampled
		assert.True(t, span.IsRecording())
		assert.True(t, span.SpanContext().IsSampled())
		span.End()

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.Equal(t, "server-remote-parent", spans[0].Name)
	})

	// 9. Remote sampled parents remain delegated under parent-based samplers.
	t.Run("Scenario 9: Remote sampled parents remain delegated under parent-based samplers", func(t *testing.T) {
		parentBasedDelegate := sdktrace.ParentBased(sdktrace.AlwaysSample())
		sampler := newRootFilteringSampler(parentBasedDelegate, true, map[string]struct{}{"/healthz": {}})
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sampler),
			sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		)
		tr := tp.Tracer("tr-9")

		// Remote sampled -> sampled
		remoteSampled := makeParentContext(true, true, trace.TraceState{})
		_, span1 := tr.Start(remoteSampled, "remote-sampled-req", trace.WithSpanKind(trace.SpanKindServer))
		assert.True(t, span1.IsRecording())
		assert.True(t, span1.SpanContext().IsSampled())
		span1.End()

		// Remote unsampled -> unsampled (ParentBased drops remote unsampled)
		remoteUnsampled := makeParentContext(true, false, trace.TraceState{})
		_, span2 := tr.Start(remoteUnsampled, "remote-unsampled-req", trace.WithSpanKind(trace.SpanKindServer))
		assert.False(t, span2.IsRecording())
		assert.False(t, span2.SpanContext().IsSampled())
		span2.End()

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.Equal(t, "remote-sampled-req", spans[0].Name)
	})

	// 10. Ratio sampling decisions remain deterministic and compatible.
	t.Run("Scenario 10: Ratio sampling decisions remain deterministic and compatible", func(t *testing.T) {
		ratioSampler := sdktrace.TraceIDRatioBased(0.5)
		filteringSampler := newRootFilteringSampler(ratioSampler, true, map[string]struct{}{"/healthz": {}})

		// For root SERVER with non-excluded path, filtering sampler should match ratio delegate exactly
		tidLow, _ := trace.TraceIDFromHex("00000000000000000000000000000001")
		tidHigh, _ := trace.TraceIDFromHex("ffffffffffffffff0000000000000000")

		paramsLow := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			TraceID:       tidLow,
			Kind:          trace.SpanKindServer,
			Attributes:    []attribute.KeyValue{attribute.String("url.path", "/api/data")},
		}
		paramsHigh := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			TraceID:       tidHigh,
			Kind:          trace.SpanKindServer,
			Attributes:    []attribute.KeyValue{attribute.String("url.path", "/api/data")},
		}

		resRatioLow := ratioSampler.ShouldSample(paramsLow)
		resFiltLow := filteringSampler.ShouldSample(paramsLow)
		assert.Equal(t, resRatioLow.Decision, resFiltLow.Decision)

		resRatioHigh := ratioSampler.ShouldSample(paramsHigh)
		resFiltHigh := filteringSampler.ShouldSample(paramsHigh)
		assert.Equal(t, resRatioHigh.Decision, resFiltHigh.Decision)
	})

	// 11. TraceState survives filtered span creation and descendant propagation.
	t.Run("Scenario 11: TraceState survives filtered span creation and descendant propagation", func(t *testing.T) {
		ts, err := trace.ParseTraceState("vendor=opaque,service=checkout")
		require.NoError(t, err)

		sampler := newRootFilteringSampler(sdktrace.AlwaysSample(), true, map[string]struct{}{"/healthz": {}})
		tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sampler))
		tr := tp.Tracer("tr-11")

		// Create a local unsampled parent that carries TraceState
		parentCtx := makeParentContext(false, false, ts)
		ctx, childSpan := tr.Start(parentCtx, "child-op", trace.WithSpanKind(trace.SpanKindInternal))
		assert.False(t, childSpan.IsRecording())
		assert.Equal(
			t,
			ts,
			childSpan.SpanContext().TraceState(),
			"TraceState must be preserved on suppressed child span",
		)

		// Descendant creation from the suppressed child's context
		_, grandchildSpan := tr.Start(ctx, "grandchild-op", trace.WithSpanKind(trace.SpanKindClient))
		assert.False(t, grandchildSpan.IsRecording())
		assert.Equal(t, ts, grandchildSpan.SpanContext().TraceState(), "TraceState must survive descendant propagation")

		grandchildSpan.End()
		childSpan.End()
	})

	// 12. Application behavior remains unaffected by telemetry suppression.
	t.Run("Scenario 12: Application behavior remains unaffected by telemetry suppression", func(t *testing.T) {
		sampler := newRootFilteringSampler(sdktrace.AlwaysSample(), true, map[string]struct{}{"/healthz": {}})
		tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sampler))
		tr := tp.Tracer("tr-12")

		// Simulate HTTP handler for /healthz where span is suppressed
		type contextKey string
		const userKey contextKey = "user_info"

		handler := func(ctx context.Context) (int, string, error) {
			ctx, span := tr.Start(ctx, "GET /healthz",
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(attribute.String("url.path", "/healthz")),
			)
			defer span.End()

			// Application context values and operations must work identically
			ctx = context.WithValue(ctx, userKey, "health-checker")
			assert.Equal(t, "health-checker", ctx.Value(userKey))

			// Simulate child DB check
			ctx2, dbSpan := tr.Start(ctx, "SELECT 1", trace.WithSpanKind(trace.SpanKindClient))
			defer dbSpan.End()

			select {
			case <-ctx2.Done():
				return 500, "timeout", ctx2.Err()
			default:
				return 200, "OK", nil
			}
		}

		code, body, err := handler(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 200, code)
		assert.Equal(t, "OK", body)
	})
}

func BenchmarkRootFilteringSampler(b *testing.B) {
	delegate := sdktrace.AlwaysSample()
	sampler := newRootFilteringSampler(delegate, true, map[string]struct{}{
		"/healthz": {},
		"/readyz":  {},
	})

	clientParams := sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Kind:          trace.SpanKindClient,
		Name:          "client-query",
	}

	producerParams := sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Kind:          trace.SpanKindProducer,
		Name:          "producer-send",
	}

	excludedServerParams := sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Kind:          trace.SpanKindServer,
		Name:          "GET /healthz",
		Attributes:    []attribute.KeyValue{attribute.String("url.path", "/healthz")},
	}

	normalServerParams := sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Kind:          trace.SpanKindServer,
		Name:          "GET /api/v1/items",
		Attributes:    []attribute.KeyValue{attribute.String("url.path", "/api/v1/items")},
	}

	sampledParentCtx := makeParentContext(false, true, trace.TraceState{})
	childParams := sdktrace.SamplingParameters{
		ParentContext: sampledParentCtx,
		Kind:          trace.SpanKindClient,
		Name:          "db-child-query",
	}

	disabledSampler := newRootFilteringSampler(delegate, false, nil)

	b.Run("RootClient_Filtered", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = sampler.ShouldSample(clientParams)
		}
	})

	b.Run("RootProducer_Filtered", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = sampler.ShouldSample(producerParams)
		}
	})

	b.Run("RootServer_ExcludedPath", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = sampler.ShouldSample(excludedServerParams)
		}
	})

	b.Run("RootServer_Normal", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = sampler.ShouldSample(normalServerParams)
		}
	})

	b.Run("ChildSpan_Normal", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = sampler.ShouldSample(childParams)
		}
	})

	b.Run("FilteringDisabled", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = disabledSampler.ShouldSample(normalServerParams)
		}
	})
}
