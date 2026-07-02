// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModuleVersion(t *testing.T) {
	version := ModuleVersion()
	// In test mode, version should be "dev" since there's no proper build info
	assert.NotEmpty(t, version)
}
