// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileExpression_UnknownTemplateTag(t *testing.T) {
	// text/template rejects an unrecognized bare identifier like "something"
	// at parse time (as an undefined function call), so newCallTemplate is
	// where the error now surfaces rather than compileExpression.
	_, err := newCallTemplate("wrapper({{ something }})")
	require.Error(t, err)
}

func TestParseGoExpression_NonExpressionStatement(t *testing.T) {
	_, err := parseGoExpression("return")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not parse as an expression statement")
}

func TestParseGoExpression_EmptyBody(t *testing.T) {
	_, err := parseGoExpression("")
	require.Error(t, err)
}

func TestParseGoTypeExpression_NoType(t *testing.T) {
	// "var _ = 1" has no explicit type on the value spec, so the parsed shape
	// must be rejected rather than silently producing a nil type.
	_, err := parseGoTypeExpression("= 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected spec shape")
}
