// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripBuildIgnore(t *testing.T) {
	assert.Equal(t, "\n\npackage main\n", StripBuildIgnore("//go:build ignore\n\npackage main\n"))
	assert.Equal(t, "package main\n", StripBuildIgnore("package main\n"))
}
