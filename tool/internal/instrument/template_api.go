//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

// !!!Any modification MUST be synced with pkg/inst/context.go
type HookContext interface {
	// Set the skip call flag, can be used to skip the original function call
	SetSkipCall(bool)
	// Get the skip call flag, can be used to skip the original function call
	IsSkipCall() bool
	// Set the data field, can be used to pass information between Before and After hooks
	SetData(interface{})
	// Get the data field, can be used to pass information between Before and After hooks
	GetData() interface{}
	// Number of original function parameters
	GetParamCount() int
	// Get the original function parameter at index idx
	GetParam(idx int) interface{}
	// Change the original function parameter at index idx
	SetParam(idx int, val interface{})
	// Number of original function return values
	GetReturnValCount() int
	// Get the original function return value at index idx
	GetReturnVal(idx int) interface{}
	// Change the original function return value at index idx
	SetReturnVal(idx int, val interface{})
	// Get the original function name
	GetFuncName() string
	// Get the package name of the original function
	GetPackageName() string
}
