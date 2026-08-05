// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// context.go is the source of truth that api.tmpl mirrors; it lives in the
// sibling pkg/ module, so the path is relative to this package dir.
const hookContextSource = "../../../pkg/hook/context.go"

// TestAPITemplateMatchesHookContext keeps api.tmpl byte-identical to
// pkg/hook/context.go (issue #899). api.tmpl is embedded and parsed for its
// HookContext decls, so any drift silently desyncs the generated interface.
// We compare the embedded templateAPI, i.e. the exact bytes the tool builds in.
func TestAPITemplateMatchesHookContext(t *testing.T) {
	source, err := os.ReadFile(hookContextSource)
	require.NoError(t, err)

	require.Equal(t, string(source), templateAPI,
		"%s and api.tmpl have drifted; re-sync with `make build` "+
			"(or `cp %s tool/internal/instrument/api.tmpl`)",
		hookContextSource, hookContextSource)
}
