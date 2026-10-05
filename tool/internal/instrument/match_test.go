// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestLoadMissingMatchedRules(t *testing.T) {
	// Point the work dir at an empty directory: matched.json does not exist,
	// which is what a bare -toolexec build sees when setup never ran.
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	ip := &instrumentPhase{logger: slog.Default()}
	_, err := ip.load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "otelc setup")
}
