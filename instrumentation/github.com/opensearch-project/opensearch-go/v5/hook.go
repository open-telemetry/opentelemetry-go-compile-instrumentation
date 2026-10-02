// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package v5 provides compile-time OpenTelemetry instrumentation for
// github.com/opensearch-project/opensearch-go/v5.
//
// (*Client).Request is the shared HTTP path for Search, Index, Bulk, Delete,
// and the rest of the public client API. Each call becomes one CLIENT span
// named "{operation} {index}" with db.system.name=opensearch.
//
// Node discovery and health checks live on the transport and never reach
// Request, so they do not create OpenSearch spans. If net/http client
// instrumentation is enabled independently, it can still trace that traffic.
//
// The request context is replaced with one that carries the new span and
// suppresses net/http client spans so the datastore hop is not duplicated
// as a generic GET/POST.
package v5

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/opensearch-project/opensearch-go/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	osemconv "go.opentelemetry.io/otelc/instrumentation/github.com/opensearch-project/opensearch-go/v5/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/opensearch-project/opensearch-go/v5"
	instrumentationKey  = "OPENSEARCH"

	// reqParamIndex is Request's only argument. HookContext parameter indexes
	// include the receiver at 0, so req is at 1. otelc matches BeforeRequest's
	// remaining params to the target at inject time, so a signature change fails
	// the instrumented build.
	reqParamIndex = 1
)

var (
	logger   = runtime.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

type openSearchEnabler struct{}

func (openSearchEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = openSearchEnabler{}

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		logger.Info("opensearch-go v5 client instrumentation initialized")
	})
}

type hookData struct {
	span trace.Span
}

// BeforeRequest runs before (*Client).Request.
func BeforeRequest(
	ictx hook.HookContext,
	_ *opensearch.Client,
	req *http.Request,
) {
	if !enabler.Enable() {
		logger.Debug("opensearch instrumentation disabled")
		return
	}
	if req == nil || req.URL == nil {
		return
	}
	initInstrumentation()

	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	operation, index := parseOpenSearchPath(req.Method, req.URL.Path)
	spanCtx, span := tracer.Start(ctx,
		osemconv.SpanName(operation, index),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(osemconv.ClientTraceAttrs(osemconv.Request{
			Method:        req.Method,
			Path:          req.URL.Path,
			Operation:     operation,
			Index:         index,
			ServerAddress: req.URL.Hostname(),
			ServerPort:    portOf(req.URL),
		})...),
	)
	spanCtx = runtime.SuppressHTTPClientInstrumentation(spanCtx)
	ictx.SetParam(reqParamIndex, req.WithContext(spanCtx))
	ictx.SetData(&hookData{span: span})

	logger.Debug("BeforeRequest",
		"method", req.Method,
		"path", req.URL.Path,
		"operation", operation,
		"index", index,
	)
}

// AfterRequest runs after (*Client).Request and ends the span.
func AfterRequest(ictx hook.HookContext, resp *http.Response, err error) {
	data, ok := ictx.GetData().(*hookData)
	if !ok || data == nil || data.span == nil {
		logger.Debug("AfterRequest: no span from before hook")
		return
	}
	span := data.span
	defer span.End()

	status := 0
	if resp != nil && resp.StatusCode > 0 {
		status = resp.StatusCode
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(semconv.ErrorTypeKey.String(openSearchErrorType(err, status)))
		logger.Debug("AfterRequest error", "error", err)
	} else if status >= 400 {
		span.SetStatus(codes.Error, "")
		span.SetAttributes(semconv.ErrorTypeKey.String(strconv.Itoa(status)))
	}

	if status > 0 {
		span.SetAttributes(semconv.DBResponseStatusCode(strconv.Itoa(status)))
	}
}

// openSearchErrorType returns a low-cardinality error.type value. An HTTP
// response code takes precedence over the Go error type because it is the
// database response status exposed by opensearch-go.
func openSearchErrorType(err error, status int) string {
	if status >= 400 {
		return strconv.Itoa(status)
	}
	if err != nil {
		return fmt.Sprintf("%T", err)
	}
	return "_OTHER"
}

// portOf returns the explicit port in u, or 0 when the URL uses a default
// scheme port (or has no host). Semantics follow the HTTP server.address
// conventions: default ports are omitted, not materialized.
func portOf(u *url.URL) int {
	if u == nil {
		return 0
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			return n
		}
	}
	return 0
}

// parseOpenSearchPath extracts the OpenSearch operation and target index from
// a REST method + path. The first path segment that does not start with '_'
// is the index. The operation comes from a later '_'-prefixed action
// (search, bulk, …) or, for document routes (/_doc, /_create), from the
// HTTP method.
func parseOpenSearchPath(method, path string) (operation, index string) {
	path = unescapePath(path)
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	segs := splitPath(path)
	rest := segs
	if len(segs) > 0 && !strings.HasPrefix(segs[0], "_") {
		index = segs[0]
		rest = segs[1:]
	}
	return operationFrom(method, rest), index
}

func unescapePath(path string) string {
	if u, err := url.PathUnescape(path); err == nil {
		return u
	}
	return path
}

func splitPath(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	raw := strings.Split(path, "/")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func operationFrom(method string, segs []string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if op := lastKnownAction(segs); op != "" {
		return op
	}
	if hasDocType(segs) {
		return docOperation(method, segs)
	}
	if clusterOp := clusterStyleOp(segs); clusterOp != "" {
		return clusterOp
	}
	return methodFallback(method)
}

func lastKnownAction(segs []string) string {
	var found string
	for _, s := range segs {
		if !strings.HasPrefix(s, "_") {
			continue
		}
		if op, ok := knownActions[strings.TrimPrefix(s, "_")]; ok {
			found = op
		}
	}
	return found
}

func hasDocType(segs []string) bool {
	for _, s := range segs {
		if s == "_doc" || s == "_create" {
			return true
		}
	}
	return false
}

func docOperation(method string, segs []string) string {
	for _, s := range segs {
		if s == "_create" {
			return "create"
		}
	}
	switch method {
	case "GET", "HEAD":
		return "get"
	case "DELETE":
		return "delete"
	default:
		// PUT is a full index. POST is index with an auto-id.
		// OpenSearch _doc does not use other methods.
		return "index"
	}
}

func clusterStyleOp(segs []string) string {
	if len(segs) == 0 || !strings.HasPrefix(segs[0], "_") {
		return ""
	}
	head := strings.TrimPrefix(segs[0], "_")
	if head == "" {
		return ""
	}
	if sub := knownClusterSub(segs[1:]); sub != "" {
		return head + "." + sub
	}
	return head
}

func knownClusterSub(segs []string) string {
	for _, s := range segs {
		name := strings.TrimPrefix(s, "_")
		if _, ok := knownClusterSubs[name]; ok {
			return name
		}
	}
	return ""
}

func methodFallback(method string) string {
	switch method {
	case "HEAD":
		return "exists"
	case "PUT":
		return "create"
	case "DELETE":
		return "delete"
	case "GET":
		return "get"
	case "POST":
		return "index"
	case "":
		return ""
	default:
		return strings.ToLower(method)
	}
}

// knownActions maps the last '_' path segment of a request to db.operation.name.
// _doc / _create are handled separately because the HTTP method selects the op.
var knownActions = map[string]string{
	"search":                 "search",
	"msearch":                "msearch",
	"search_template":        "search_template",
	"render_search_template": "render_search_template",
	"bulk":                   "bulk",
	"count":                  "count",
	"update":                 "update",
	"update_by_query":        "update_by_query",
	"delete_by_query":        "delete_by_query",
	"mget":                   "mget",
	"mtermvectors":           "mtermvectors",
	"refresh":                "refresh",
	"flush":                  "flush",
	"mapping":                "mapping",
	"settings":               "settings",
	"aliases":                "aliases",
	"scroll":                 "scroll",
	"clear_scroll":           "clear_scroll",
	"explain":                "explain",
	"validate":               "validate",
	"reindex":                "reindex",
	"pit":                    "pit",
	"search_pipeline":        "search_pipeline",
	"segment_replication":    "segment_replication",
}

// knownClusterSubs is the second (or later) path segment after a leading
// '_' cluster-style prefix. Dynamic ids such as /_tasks/{id} stay out.
var knownClusterSubs = map[string]struct{}{
	"health":                 {},
	"stats":                  {},
	"info":                   {},
	"settings":               {},
	"state":                  {},
	"http":                   {},
	"allocation":             {},
	"pending_tasks":          {},
	"reroute":                {},
	"hot_threads":            {},
	"usage":                  {},
	"nodes":                  {},
	"plugins":                {},
	"ingest":                 {},
	"indices":                {},
	"tasks":                  {},
	"snapshot":               {},
	"weighted_routing":       {},
	"decommission_awareness": {},
	"remote_store":           {},
	// _cat sub-APIs are fixed names; dynamic ids stay out.
	"cluster_manager": {},
	"fielddata":       {},
	"master":          {},
	"nodeattrs":       {},
	"recovery":        {},
	"repositories":    {},
	"segments":        {},
	"shards":          {},
	"snapshots":       {},
	"templates":       {},
	"thread_pool":     {},
	"count":           {},
	"aliases":         {},
	"version":         {},
}
