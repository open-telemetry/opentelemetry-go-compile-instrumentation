// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAssertType_Success(t *testing.T) {
	// Concrete type
	val := AssertType[int](123)
	assert.Equal(t, 123, val)

	// Pointer type
	s := "hello"
	ps := AssertType[*string](&s)
	assert.Equal(t, &s, ps)
	assert.Equal(t, "hello", *ps)
}

func TestAssertType_NilPointer(t *testing.T) {
	var s *string

	ps := AssertType[*string](s)
	assert.Nil(t, ps)
}
