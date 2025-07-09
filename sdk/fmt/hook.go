// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sdk

import (
	_ "unsafe"
)

//go:linkname MyFmtHookBefore fmt.MyFmtHookBefore
func MyFmtHookBefore(ctx inst.Context) {
	targetFuncName := ctx.GetFuncName()
	targetPackageName := ctx.GetPackageName()
	println("Before", targetFuncName, targetPackageName)
}

//go:linkname MyFmtHookAfter fmt.MyFmtHookAfter
func MyFmtHookAfter(ctx inst.Context) {
	targetFuncName := ctx.GetFuncName()
	targetPackageName := ctx.GetPackageName()
	println("After", targetFuncName, targetPackageName)
}
