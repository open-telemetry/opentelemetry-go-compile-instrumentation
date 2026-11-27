// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/call-rule-shared-alias-unused-import/helpers/lib"

// The A and B calls are the only uses of the lib import in this file.
func main() {
	lib.A()
	lib.B()
}
