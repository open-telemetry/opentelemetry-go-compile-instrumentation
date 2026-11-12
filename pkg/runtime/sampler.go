// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	envDropRootClientProducer = "OTEL_GO_DROP_ROOT_CLIENT_PRODUCER"
	envExcludedServerPaths    = "OTEL_GO_EXCLUDED_SERVER_PATHS"

	tracesSamplerKey    = "OTEL_TRACES_SAMPLER"
	tracesSamplerArgKey = "OTEL_TRACES_SAMPLER_ARG"

	samplerAlwaysOn                = "always_on"
	samplerAlwaysOff               = "always_off"
	samplerTraceIDRatio            = "traceidratio"
	samplerParentBasedAlwaysOn     = "parentbased_always_on"
	samplerParentBasedAlwaysOff    = "parentbased_always_off"
	samplerParentBasedTraceIDRatio = "parentbased_traceidratio"

	urlPathKey = attribute.Key("url.path")
)

// parseExcludedPaths parses a comma-separated list of paths into a set.
// It trims whitespace around entries and ignores empty entries.
func parseExcludedPaths(raw string) map[string]struct{} {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	paths := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			paths[trimmed] = struct{}{}
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return paths
}

// parseDropRootClientProducer returns true if raw is case-insensitively "true".
func parseDropRootClientProducer(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), "true")
}

// rootFilteringSampler wraps a delegate sampler to suppress parentless CLIENT/PRODUCER spans
// and root SERVER spans matching configured paths, as well as their local descendants.
//
// Trade-off note: Any span with a valid local unsampled parent is dropped. For non-parent-based
// samplers like always_on, this differs from the delegate's normal behavior for unrelated
// locally unsampled parents. This trade-off avoids mutable trace-lifecycle state or global tracking.
type rootFilteringSampler struct {
	delegate               sdktrace.Sampler
	dropRootClientProducer bool
	excludedPaths          map[string]struct{}
	description            string
}

var _ sdktrace.Sampler = (*rootFilteringSampler)(nil)

func newRootFilteringSampler(
	delegate sdktrace.Sampler,
	dropRootClientProducer bool,
	excludedPaths map[string]struct{},
) *rootFilteringSampler {
	var pathList []string
	if len(excludedPaths) > 0 {
		pathList = make([]string, 0, len(excludedPaths))
		for p := range excludedPaths {
			pathList = append(pathList, p)
		}
		sort.Strings(pathList)
	}

	desc := fmt.Sprintf("RootFilteringSampler{dropRootClientProducer:%t,excludedPaths:[%s],delegate:%s}",
		dropRootClientProducer,
		strings.Join(pathList, ","),
		delegate.Description(),
	)

	return &rootFilteringSampler{
		delegate:               delegate,
		dropRootClientProducer: dropRootClientProducer,
		excludedPaths:          excludedPaths,
		description:            desc,
	}
}

func dropResult(psc trace.SpanContext) sdktrace.SamplingResult {
	return sdktrace.SamplingResult{
		Decision:   sdktrace.Drop,
		Tracestate: psc.TraceState(),
	}
}

func (s *rootFilteringSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	var psc trace.SpanContext
	if p.ParentContext != nil {
		psc = trace.SpanContextFromContext(p.ParentContext)
	}

	if psc.IsValid() {
		// Descendant suppression: if the valid parent is local and unsampled,
		// suppress this child span so descendants of dropped local roots are not recorded.
		if !psc.IsRemote() && !psc.IsSampled() {
			return dropResult(psc)
		}
		return s.delegate.ShouldSample(p)
	}

	// This is a root span (no valid parent context).
	if s.dropRootClientProducer && (p.Kind == trace.SpanKindClient || p.Kind == trace.SpanKindProducer) {
		return dropResult(psc)
	}

	if len(s.excludedPaths) > 0 && p.Kind == trace.SpanKindServer {
		for _, attr := range p.Attributes {
			if attr.Key == urlPathKey {
				if _, ok := s.excludedPaths[attr.Value.AsString()]; ok {
					return dropResult(psc)
				}
			}
		}
	}

	return s.delegate.ShouldSample(p)
}

func (s *rootFilteringSampler) Description() string {
	return s.description
}

// newFilteringSamplerFromEnv inspects environment variables and constructs a rootFilteringSampler
// wrapping the SDK environment sampler if any filtering is enabled. Returns nil if filtering is disabled.
func newFilteringSamplerFromEnv() sdktrace.Sampler {
	dropClientProducer := parseDropRootClientProducer(os.Getenv(envDropRootClientProducer))
	excludedPaths := parseExcludedPaths(os.Getenv(envExcludedServerPaths))
	if !dropClientProducer && len(excludedPaths) == 0 {
		return nil
	}
	baseSampler := sdkSamplerFromEnv()
	return newRootFilteringSampler(baseSampler, dropClientProducer, excludedPaths)
}

// sdkSamplerFromEnv reproduces the OpenTelemetry Go SDK's environment-based sampler resolution.
// It avoids calling otel.Handle(err) directly because sdktrace.NewTracerProvider will also inspect
// the environment during applyTracerProviderEnvConfigs and invoke otel.Handle(err) once.
func sdkSamplerFromEnv() sdktrace.Sampler {
	sampler := samplerFromEnv()
	if sampler == nil {
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
	return sampler
}

func samplerFromEnv() sdktrace.Sampler {
	sampler := strings.ToLower(strings.TrimSpace(os.Getenv(tracesSamplerKey)))
	if sampler == "" {
		return nil
	}

	samplerArg := strings.TrimSpace(os.Getenv(tracesSamplerArgKey))
	hasSamplerArg := samplerArg != ""

	switch sampler {
	case samplerAlwaysOn:
		return sdktrace.AlwaysSample()
	case samplerAlwaysOff:
		return sdktrace.NeverSample()
	case samplerTraceIDRatio:
		if !hasSamplerArg {
			return sdktrace.TraceIDRatioBased(1.0)
		}
		return parseTraceIDRatio(samplerArg)
	case samplerParentBasedAlwaysOn:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case samplerParentBasedAlwaysOff:
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case samplerParentBasedTraceIDRatio:
		if !hasSamplerArg {
			return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0))
		}
		return sdktrace.ParentBased(parseTraceIDRatio(samplerArg))
	default:
		return nil
	}
}

func parseTraceIDRatio(arg string) sdktrace.Sampler {
	v, err := strconv.ParseFloat(arg, 64)
	if err != nil || v < 0.0 || v > 1.0 {
		return sdktrace.TraceIDRatioBased(1.0)
	}
	return sdktrace.TraceIDRatioBased(v)
}
