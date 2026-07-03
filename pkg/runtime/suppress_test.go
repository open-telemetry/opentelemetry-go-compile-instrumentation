// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTTPClientInstrumentationSuppression(t *testing.T) {
	ctx := context.Background()

	// A plain context carries no suppression flag.
	assert.False(t, IsHTTPClientInstrumentationSuppressed(ctx))

	// Once suppressed, the returned context reports the flag.
	suppressed := SuppressHTTPClientInstrumentation(ctx)
	assert.True(t, IsHTTPClientInstrumentationSuppressed(suppressed))

	// Suppression does not leak back into the original context.
	assert.False(t, IsHTTPClientInstrumentationSuppressed(ctx))
}
