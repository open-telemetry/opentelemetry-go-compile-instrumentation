// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package wrap

import "go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/call-rule-unused-import/helpers/lib"

// New returns a traced lib.Client.
func New(name string) *lib.Client {
	c := lib.New(name)
	c.Traced = true
	return c
}
