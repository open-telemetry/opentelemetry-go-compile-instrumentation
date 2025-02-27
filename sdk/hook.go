// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sdk

import (
	"fmt"
	_ "unsafe"
)

//go:linkname MyHook main.Hook
func MyHook() {
	fmt.Printf("Entering hook!\n")
}
