// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInstBaseRule_GetExcludeTargets(t *testing.T) {
	t.Parallel()

	base := InstBaseRule{
		ExcludeTargets: []string{"example.com/excluded"},
	}
	assert.Equal(t, []string{"example.com/excluded"}, base.GetExcludeTargets())
}
