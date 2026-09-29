// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"

	"github.com/gorilla/mux"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	httpsemconv "go.opentelemetry.io/otelc/instrumentation/net/http/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
)

// AfterMatch runs after (*Router).Match. The after hook sees only the bool
// return. Params are fixed: 0 is the *Router, 1 is *http.Request, 2 is
// *mux.RouteMatch.
//
// The bool is the match result. Return when it is false even if Route is
// set and MatchErr is nil. A leftover or custom Route on a failed Match
// must not receive http.route.
//
// Match returns true for a custom NotFoundHandler or MethodNotAllowedHandler
// as well as for a real route. Those cases set MatchErr (ErrNotFound /
// ErrMethodMismatch) and must not receive http.route — same as a 404/405
// that used mux's default handlers.
func AfterMatch(ictx hook.HookContext, ok bool) {
	if !enabler.Enable() || ictx == nil || !ok {
		return
	}

	req, _ := ictx.GetParam(1).(*http.Request)
	match, _ := ictx.GetParam(2).(*mux.RouteMatch)
	if req == nil || match == nil || match.MatchErr != nil || match.Route == nil {
		return
	}

	route, err := match.Route.GetPathTemplate()
	if err != nil || route == "" {
		return
	}

	span := trace.SpanFromContext(req.Context())
	if !span.IsRecording() {
		return
	}

	span.SetName(httpsemconv.HTTPServerSpanName(req.Method, route))
	span.SetAttributes(semconv.HTTPRoute(route))
	logger.Debug("mux route resolved", "route", route)
}
