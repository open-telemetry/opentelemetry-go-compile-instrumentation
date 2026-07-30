// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// InterfacePassthrough has an interface{} parameter and an "any" return value.
// Interface-typed slots are passthrough slots in the generated SetParam and
// SetReturnVal switches: the value is stored directly with no pointer
// indirection and no nil special-casing (exercises setValue's IsInterfaceType
// branch).
func InterfacePassthrough(v interface{}) (r any) {
	return v
}

func main() {}
