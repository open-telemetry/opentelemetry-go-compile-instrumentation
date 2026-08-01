// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// api.tmpl is a hand-copied version of this file.
const hookContextSourcePath = "../../../pkg/hook/context.go"

// api.tmpl must be a byte-for-byte copy of pkg/hook/context.go, or generated
// hook code ends up with a HookContext that doesn't match the real one.
func TestAPITemplateMatchesHookContext(t *testing.T) {
	want, err := os.ReadFile(hookContextSourcePath)
	require.NoError(t, err, "reading %s", hookContextSourcePath)

	assert.Equal(t, string(want), templateAPI, "api.tmpl has drifted from pkg/hook/context.go")
}
