// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/call-rule-unused-import/helpers/lib"

// The lib.New call is the only use of the lib import in this file.
func newClient() traced { return tracedOf(lib.New("a")) }

func main() {
	println(newClient().IsTraced(), otherClient().Traced)
}
