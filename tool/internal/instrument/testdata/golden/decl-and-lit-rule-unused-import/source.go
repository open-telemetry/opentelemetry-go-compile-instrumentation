// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/decl-and-lit-rule-unused-import/helpers/lib"
	"go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/decl-and-lit-rule-unused-import/helpers/transport"
	"go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/decl-and-lit-rule-unused-import/helpers/wrap"
)

// The lib.Default call is the only use of the lib import.
var Client = lib.Default()

// The transport.New call is the only use of the transport import.
var Config = &wrap.Config{Transport: transport.New()}

func main() {
	println(Client.Traced, Config.Transport != nil)
}
