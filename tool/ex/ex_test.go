// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ex

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestError(t *testing.T) {
	err := Newf("a")
	err = Wrapf(err, "b")
	err = Wrap(Wrap(Wrap(err))) // make no sense
	require.Contains(t, err.Error(), "a")
	require.Contains(t, err.Error(), "b")

	err = errors.New("c")
	err = Wrapf(err, "d")
	err = Wrapf(err, "e")
	err = Wrap(Wrap(Wrap(err))) // make no sense
	require.Contains(t, err.Error(), "c")
	require.Contains(t, err.Error(), "d")
}

func TestJoinStackful(t *testing.T) {
	e1 := New("first")
	e2 := Newf("second %d", 2)
	joined := Join(e1, e2)

	require.True(t, errors.Is(joined, e1))
	require.True(t, errors.Is(joined, e2))

	var se *stackfulError
	require.True(t, errors.As(joined, &se))
}

func TestJoinMixed(t *testing.T) {
	stdErr := errors.New("std")
	exErr := New("ex")
	joined := Join(stdErr, exErr)

	require.True(t, errors.Is(joined, stdErr))
	require.True(t, errors.Is(joined, exErr))

	var se *stackfulError
	require.True(t, errors.As(joined, &se))
	require.Contains(t, se.Error(), "ex")
}
