// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// interface{}/any params and returns take the passthrough path in the
// generated SetParam/SetReturnVal (stored directly, no pointer, no nil check).
func InterfacePassthrough(v interface{}) (r any) {
	return v
}

func main() {}
