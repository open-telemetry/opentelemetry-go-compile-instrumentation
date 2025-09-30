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
