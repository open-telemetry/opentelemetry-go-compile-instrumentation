// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoPin_Coverage(t *testing.T) {
	ctx := context.Background()
	_, cleanup, err := AutoPin(ctx, map[string]bool{}, []string{})
	require.NoError(t, err)
	if cleanup != nil {
		cleanup()
	}
}
