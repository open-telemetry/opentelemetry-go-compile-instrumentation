// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// allowlist lists unit test files that intentionally do not pair 1:1 with a
// same-named source file, together with the reason each one can't simply be
// renamed or split to satisfy the exact-match rule. Keeping the reason next
// to the entry is what keeps this a registry of deliberate exceptions rather
// than an escape hatch for the segmented test files the naming rule exists
// to prevent (see the discussion on #1128). Paths are slash-separated,
// relative to the repository root.
// seeV1PackageRationale is shared by the v2/v3 openai-go allowlist entries,
// which mirror their v1 counterpart's rationale exactly.
const seeV1PackageRationale = "see the v1 package rationale."

var allowlist = map[string]string{ //nolint:gochecknoglobals // private lookup table
	"tool/internal/instrument/toolexec_exec_test.go": "exercises the !windows subprocess-exec path of toolexec.go; " +
		"toolexec_test.go already covers the shared, platform-independent behavior.",
	"tool/internal/instrument/render_raw_code_test.go": "pending fold into apply_raw_test.go (open PR #1189); " +
		"tracked there instead of duplicated here.",
	"tool/internal/instrument/instrument_delegate_test.go": "covers the thin slog delegator methods on instrumentPhase, " +
		"which live in instrument.go; named for the behavior under test rather than the source file.",
	"tool/util/assert_fatal_test.go": "pending fold into assert_test.go (open PR #1189); " +
		"tracked there instead of duplicated here.",

	"instrumentation/database/sql/dsnparse/parse_addr_test.go": "cross-dialect contract test asserting parser output " +
		"composes correctly with the semconv Addr builder; not scoped to a single parse.go function.",
	"instrumentation/database/sql/dsnparse/parse_dialects_test.go": "per-dialect DSN parsing cases split out of " +
		"parse_test.go for readability; all exercise parse.go.",
	"instrumentation/database/sql/dsnparse/parse_fuzz_test.go": "fuzz target for parse.go's DSN parsers, kept in its " +
		"own file by Go fuzz-testing convention.",
	"instrumentation/database/sql/semconv/db_ipv6_test.go": "IPv6-specific edge cases for db.go's attribute builder, " +
		"split out of db_test.go for readability.",

	"instrumentation/github.com/openai/openai-go/middleware_integration_test.go": "end-to-end test driving the " +
		"full middleware.go pipeline against a real HTTP round trip, distinct from middleware_test.go's unit cases.",
	"instrumentation/github.com/openai/openai-go/v2/middleware_integration_test.go": seeV1PackageRationale,
	"instrumentation/github.com/openai/openai-go/v3/middleware_integration_test.go": seeV1PackageRationale,
	"instrumentation/github.com/openai/openai-go/testhelpers_test.go": "shared test-only attribute-assertion helpers; " +
		"no corresponding production source file.",
	"instrumentation/github.com/openai/openai-go/v2/testhelpers_test.go": seeV1PackageRationale,
	"instrumentation/github.com/openai/openai-go/v3/testhelpers_test.go": seeV1PackageRationale,

	"instrumentation/net/http/client/propagation_test.go": "end-to-end trace-context propagation test spanning " +
		"client_hook.go and server_hook.go behavior together; not scoped to one source file.",
	"instrumentation/net/http/server/propagation_test.go": "see the client package rationale.",
}
