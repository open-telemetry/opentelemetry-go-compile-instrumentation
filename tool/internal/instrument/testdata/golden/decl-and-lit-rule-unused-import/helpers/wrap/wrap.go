// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package wrap

import (
	"go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/decl-and-lit-rule-unused-import/helpers/lib"
	"go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/decl-and-lit-rule-unused-import/helpers/transport"
)

// Config holds a transport.
type Config struct{ Transport any }

// Default returns a traced lib.Client.
func Default() *lib.Client { return &lib.Client{Traced: true} }

// Transport returns a traced transport.Transport.
func Transport() any { return &transport.Transport{Traced: true} }
