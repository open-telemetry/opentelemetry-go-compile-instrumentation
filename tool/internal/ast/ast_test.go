// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAst(t *testing.T) {
	_, err := ParseFile("ast_test.go")
	require.NoError(t, err)
}
