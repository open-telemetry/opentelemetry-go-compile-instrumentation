// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestAssertType_NilFailure(t *testing.T) {
	if os.Getenv("ASSERTTYPE_FATAL") == "1" {
		AssertType[*string](nil)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestAssertType_NilFailure")
	cmd.Env = append(os.Environ(), "ASSERTTYPE_FATAL=1")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)

	assert.Contains(t,
		stderr.String(),
		"Type assertion failed: got nil, expected *string")
}

func TestAssertType_InvalidType(t *testing.T) {
	if os.Getenv("ASSERTTYPE_INVALID") == "1" {
		AssertType[string](123)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestAssertType_InvalidType")
	cmd.Env = append(os.Environ(), "ASSERTTYPE_INVALID=1")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)

	assert.Contains(t,
		stderr.String(),
		"Type assertion failed: got int, expected string")
}
