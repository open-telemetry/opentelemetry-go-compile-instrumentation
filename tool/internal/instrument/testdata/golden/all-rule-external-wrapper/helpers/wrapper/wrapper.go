// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package wrapper

// Tag is a named type used to verify that struct rules can inject external types as fields.
type Tag struct{ Value string }

// Wrapper wraps a uintptr value, used by call rules.
func Wrapper(size uintptr) uintptr {
	return size
}

// Log is a no-op helper used by raw and directive rules to verify external import injection.
func Log(msg string) {}

// WrapValue wraps an interface{} value, used by decl rules.
func WrapValue(v interface{}) interface{} {
	return v
}
