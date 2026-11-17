// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// sdkTraceImports lists the imports of go.opentelemetry.io/otel/sdk/trace that
// every version since the rule's v1.44.0 floor has. An import that the target
// package does not already have cannot be added to a package in the module
// cache, so the injected file must stay within this list.
var sdkTraceImports = map[string]bool{
	"os":                             true,
	"runtime":                        true,
	"strconv":                        true,
	"sync":                           true,
	"sync/atomic":                    true,
	"go.opentelemetry.io/otel/trace": true,
}

func TestTraceContextFileAddsNoImports(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "otel_trace_context.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range f.Imports {
		// The parser only accepts valid import path literals.
		path, _ := strconv.Unquote(spec.Path.Value)
		// unsafe is required by //go:linkname and is not a real dependency.
		if path == "unsafe" {
			continue
		}
		if !sdkTraceImports[path] {
			t.Errorf("otel_trace_context.go imports %q, which go.opentelemetry.io/otel/sdk/trace does not", path)
		}
	}
}
