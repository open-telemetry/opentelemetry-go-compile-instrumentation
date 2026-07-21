// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

// OtelcVersion is set to the compile-time otelc version by the instrumentation
// phase. It falls back to "dev" for plain (non-otelc) builds or local devel
// builds where the version is not known.
//
//nolint:gochecknoglobals // overwritten by the generated init() injected at build time
var OtelcVersion = "dev"

// distroName is the telemetry.distro.name value reported on the OTel Resource.
// Official distros SHOULD use an "opentelemetry-" prefix per the OTel spec.
const distroName = "opentelemetry-go-compile-instrumentation"

// distroResource returns a *resource.Resource containing the telemetry.distro.*
// attributes for this distro. These are merged at the lowest priority in
// setupOpenTelemetry so that OTEL_RESOURCE_ATTRIBUTES can still override them.
func distroResource() *resource.Resource {
	return resource.NewWithAttributes(
		resource.Default().SchemaURL(),
		attribute.String("telemetry.distro.name", distroName),
		attribute.String("telemetry.distro.version", OtelcVersion),
	)
}
