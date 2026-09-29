// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package v7 provides compile-time OpenTelemetry instrumentation for
// github.com/olivere/elastic/v7.
//
// (*Client).PerformRequest is the shared HTTP path for Search, Index, Bulk,
// Delete, and the rest of the public client API. Each call becomes one CLIENT
// span named "{operation} {index}" with db.system.name=elasticsearch.
//
// Sniff and healthcheck in NewClient do not use PerformRequest. They call
// the inner http.Client directly, so they stay as net/http client spans.
// NewSimpleClient skips those loops.
//
// The request context is replaced with one that carries the new span and
// suppresses net/http client spans so the datastore hop is not duplicated
// as a generic GET/POST. The host is chosen inside PerformRequest after
// this hook runs, so this span does not set server.address or url.full.
package v7

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/olivere/elastic/v7"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	esemconv "go.opentelemetry.io/otelc/instrumentation/github.com/olivere/elastic/v7/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/olivere/elastic/v7"
	instrumentationKey  = "ELASTIC"

	// ctxParamIndex is PerformRequest's context argument (receiver is 0).
	// otelc matches BeforePerformRequest's remaining params to the target
	// at inject time, so a signature change fails the instrumented build.
	ctxParamIndex = 1
)

var (
	logger   = runtime.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

type elasticEnabler struct{}

func (elasticEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = elasticEnabler{}

func initInstrumentation() {
	initOnce.Do(func() {
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		)
		logger.Info("olivere/elastic v7 client instrumentation initialized")
	})
}

type hookData struct {
	span         trace.Span
	ignoreErrors []int
}

// BeforePerformRequest runs before (*Client).PerformRequest.
func BeforePerformRequest(
	ictx hook.HookContext,
	_ *elastic.Client,
	ctx context.Context,
	opt elastic.PerformRequestOptions,
) {
	if !enabler.Enable() {
		logger.Debug("olivere/elastic instrumentation disabled")
		return
	}
	initInstrumentation()

	if ctx == nil {
		ctx = context.Background()
	}

	operation, index := parseElasticPath(opt.Method, opt.Path)
	req := esemconv.Request{
		Method:    opt.Method,
		Path:      opt.Path,
		Operation: operation,
		Index:     index,
	}
	ctx, span := tracer.Start(ctx,
		esemconv.SpanName(operation, index),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(esemconv.ClientTraceAttrs(req)...),
	)
	ctx = runtime.SuppressHTTPClientInstrumentation(ctx)
	ictx.SetParam(ctxParamIndex, ctx)
	ictx.SetData(&hookData{span: span, ignoreErrors: opt.IgnoreErrors})

	logger.Debug("BeforePerformRequest",
		"method", opt.Method,
		"path", opt.Path,
		"operation", operation,
		"index", index,
	)
}

// AfterPerformRequest runs after (*Client).PerformRequest and ends the span.
func AfterPerformRequest(ictx hook.HookContext, resp *elastic.Response, err error) {
	data, ok := ictx.GetData().(*hookData)
	if !ok || data == nil || data.span == nil {
		logger.Debug("AfterPerformRequest: no span from before hook")
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
		var esErr *elastic.Error
		if errors.As(err, &esErr) && esErr.Status > 0 {
			status = esErr.Status
		}
		logger.Debug("AfterPerformRequest error", "error", err)
	} else if status >= 400 && !slices.Contains(data.ignoreErrors, status) {
		span.SetStatus(codes.Error, "")
	}

	if status > 0 {
		span.SetAttributes(semconv.DBResponseStatusCode(strconv.Itoa(status)))
	}
}

// parseElasticPath extracts the Elasticsearch operation and target index from
// a REST method + path. The first path segment that does not start with '_'
// is the index. The operation comes from a later '_'-prefixed action
// (search, bulk, …) or, for document routes (/_doc, /_create), from the
// HTTP method.
func parseElasticPath(method, path string) (operation, index string) {
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
		// Elasticsearch _doc does not use other methods.
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
	"search":          "search",
	"msearch":         "msearch",
	"bulk":            "bulk",
	"count":           "count",
	"update":          "update",
	"update_by_query": "update_by_query",
	"delete_by_query": "delete_by_query",
	"mget":            "mget",
	"refresh":         "refresh",
	"flush":           "flush",
	"mapping":         "mapping",
	"settings":        "settings",
	"aliases":         "aliases",
	"scroll":          "scroll",
	"explain":         "explain",
	"validate":        "validate",
}

// knownClusterSubs is the second (or later) path segment after a leading
// '_' cluster-style prefix. Dynamic ids such as /_tasks/{id} stay out.
var knownClusterSubs = map[string]struct{}{
	"health":        {},
	"stats":         {},
	"info":          {},
	"settings":      {},
	"state":         {},
	"http":          {},
	"allocation":    {},
	"pending_tasks": {},
	"reroute":       {},
	"hot_threads":   {},
	"usage":         {},
	"nodes":         {},
	"plugins":       {},
	"ingest":        {},
	"indices":       {},
}
