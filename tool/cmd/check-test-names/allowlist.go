// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// allowlist lists unit test files that intentionally do not pair 1:1 with a
// same-named source file. Paths are slash-separated, relative to the
// repository root.
var allowlist = []string{ //nolint:gochecknoglobals // private lookup table
	"tool/internal/instrument/toolexec_exec_test.go",   // !windows subprocess-exec path
	"tool/internal/instrument/render_raw_code_test.go", // pending fold into apply_raw_test.go (#1189)
	"tool/internal/instrument/instrument_delegate_test.go",
	"tool/util/assert_fatal_test.go", // pending fold into assert_test.go (#1189)

	"instrumentation/database/sql/dsnparse/parse_addr_test.go",
	"instrumentation/database/sql/dsnparse/parse_dialects_test.go",
	"instrumentation/database/sql/dsnparse/parse_fuzz_test.go", // fuzz target for parse.go
	"tool/internal/ast/directive_fuzz_test.go",                 // fuzz target for directive.go
	"instrumentation/database/sql/semconv/db_ipv6_test.go",

	"instrumentation/github.com/openai/openai-go/middleware_integration_test.go",
	"instrumentation/github.com/openai/openai-go/v2/middleware_integration_test.go",
	"instrumentation/github.com/openai/openai-go/v3/middleware_integration_test.go",
	"instrumentation/github.com/openai/openai-go/testhelpers_test.go",
	"instrumentation/github.com/openai/openai-go/v2/testhelpers_test.go",
	"instrumentation/github.com/openai/openai-go/v3/testhelpers_test.go",

	"instrumentation/net/http/client/propagation_test.go",
	"instrumentation/net/http/server/propagation_test.go",
}
