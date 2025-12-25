// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ex

import (
	"errors"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAndErrorMessage(t *testing.T) {
	err := New("something went wrong")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "something went wrong")
}

func TestNewf(t *testing.T) {
	err := Newf("error code: %d", 77)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error code: 77")
}

func TestWrapAndWrapf(t *testing.T) {
	baseErr := errors.New("root cause")

	wrapped := Wrap(baseErr)
	require.Error(t, wrapped)
	assert.Contains(t, wrapped.Error(), "root cause")

	wrappedf := Wrapf(baseErr, "code: %d", 77)
	require.Error(t, wrappedf)
	assert.Contains(t, wrappedf.Error(), "code: 77: root cause")
}

func TestUnwrap(t *testing.T) {
	baseErr := errors.New("root cause")
	w1 := Wrap(baseErr)
	w2 := Wrap(w1)

	assert.ErrorIs(t, w2, baseErr)
}

func TestStackTracePresent(t *testing.T) {
	err := New("root cause")

	var se *stackfulError
	require.ErrorAs(t, err, &se)
	require.NotEmpty(t, se.frame)

	pattern := regexp.MustCompile(`^\[[0-9]+].+:[0-9]+ .*`)
	for _, fr := range se.frame {
		require.Regexp(t, pattern, fr, "invalid stack frame format")
	}
}
