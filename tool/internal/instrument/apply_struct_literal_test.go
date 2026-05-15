// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func TestApplyStructLiteralRule(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		ruleYaml string
		expected string
	}{
		{
			name: "pointer match",
			source: `package main

import "net/http"

func main() {
	s := &http.Server{Addr: ":8080"}
	_ = s
}
`,
			ruleYaml: `
struct_literal: "net/http.Server"
target: "main"
match: pointer-only
template: |
  func(s *http.Server) *http.Server {
      return s
  }({{ . }})
`,
			expected: `package main

import "net/http"

func main() {
	s := func(s *http.Server) *http.Server {
		return s
	}(&http.Server{Addr: ":8080"})
	_ = s
}
`,
		},
		{
			name: "value match",
			source: `package main

import "net/http"

func main() {
	s := http.Server{Addr: ":8080"}
	_ = s
}
`,
			ruleYaml: `
struct_literal: "net/http.Server"
target: "main"
match: value-only
template: |
  func(s http.Server) http.Server {
      return s
  }({{ . }})
`,
			expected: `package main

import "net/http"

func main() {
	s := func(s http.Server) http.Server {
		return s
	}(http.Server{Addr: ":8080"})
	_ = s
}
`,
		},
		{
			name: "pointer mismatch (expects value)",
			source: `package main

import "net/http"

func main() {
	s := &http.Server{Addr: ":8080"}
	_ = s
}
`,
			ruleYaml: `
struct_literal: "net/http.Server"
target: "main"
match: value-only
template: "wrapped({{ . }})"
`,
			expected: `package main

import "net/http"

func main() {
	s := &http.Server{Addr: ":8080"}
	_ = s
}
`,
		},
		{
			name: "any match",
			source: `package main

import "net/http"

func main() {
	s := &http.Server{Addr: ":8080"}
	v := http.Server{Addr: ":9090"}
}
`,
			ruleYaml: `
struct_literal: "net/http.Server"
target: "main"
match: any
template: "wrapped({{ . }})"
`,
			expected: `package main

import "net/http"

func main() {
	s := wrapped(&http.Server{Addr: ":8080"})
	v := wrapped(http.Server{Addr: ":9090"})
}
`,
		},
		{
			name: "alias import",
			source: `package main

import myhttp "net/http"

func main() {
	s := &myhttp.Server{Addr: ":8080"}
	_ = s
}
`,
			ruleYaml: `
struct_literal: "net/http.Server"
target: "main"
template: "wrapped({{ . }})"
`,
			expected: `package main

import myhttp "net/http"

func main() {
	s := wrapped(&myhttp.Server{Addr: ":8080"})
	_ = s
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := &InstrumentPhase{
				logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			r, err := rule.NewInstStructLiteralRule([]byte(tt.ruleYaml), "test-rule")
			require.NoError(t, err)

			fset := token.NewFileSet()
			file, err := decorator.ParseFile(fset, "test.go", tt.source, 0)
			require.NoError(t, err)

			err = ip.applyStructLiteralRule(context.Background(), r, file)
			require.NoError(t, err)

			var buf strings.Builder
			err = decorator.Fprint(&buf, file)
			require.NoError(t, err)

			assert.Equal(t, tt.expected, buf.String())
		})
	}
}
