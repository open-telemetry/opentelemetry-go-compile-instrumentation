// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

// embeddedConfigurationSnapshot is set by otelc's link-time -X flag. Keeping
// the value in pkg/hook gives injected hook code a stable bridge to the
// application snapshot without making instrumentation packages depend on an
// otelc runtime implementation.
var embeddedConfigurationSnapshot string

// ConfigurationSnapshot returns the versioned, normalized JSON application
// configuration embedded by otelc. It returns an empty string when the
// application was built without selected instrumentation configuration.
// The returned string is immutable and does not read files or parse YAML.
func ConfigurationSnapshot() string {
	return embeddedConfigurationSnapshot
}
