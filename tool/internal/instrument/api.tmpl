// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

// !!! pkg/hook/context.go will auto-sync to tool/internal/instrument/api.tmpl
type HookContext interface {
	// Set the skip call flag, can be used to skip the original function call
	SetSkipCall(bool)
	// Get the skip call flag, can be used to skip the original function call
	IsSkipCall() bool
	// Set the data field, can be used to pass information between Before and After hooks.
	// SetData and SetKeyData are mutually exclusive views of the same underlying field: calling
	// SetData replaces whatever SetKeyData had stored, and calling SetKeyData after SetData(val)
	// discards val and starts a fresh key-value map. The last writer wins.
	SetData(interface{})
	// Get the data field, can be used to pass information between Before and After hooks
	GetData() interface{}
	// Get a value from the data field by key
	GetKeyData(key string) interface{}
	// Set a key-value pair in the data field. See SetData for how this interacts with it.
	SetKeyData(key string, val interface{})
	// Check if a key exists in the data field
	HasKeyData(key string) bool
	// Number of original function parameters
	GetParamCount() int
	// Get the original function parameter at index idx
	GetParam(idx int) interface{}
	// Change the original function parameter at index idx. Passing a nil val
	// stores nil for an interface-typed parameter, and resets a non-nilable
	// parameter (e.g. int or a struct) to its zero value.
	SetParam(idx int, val interface{})
	// Number of original function return values
	GetReturnValCount() int
	// Get the original function return value at index idx
	GetReturnVal(idx int) interface{}
	// Change the original function return value at index idx. Passing a nil val
	// stores nil for an interface-typed return value, and resets a non-nilable
	// return value (e.g. int or a struct) to its zero value.
	SetReturnVal(idx int, val interface{})
	// Get the original function name
	GetFuncName() string
	// Get the package name of the original function
	GetPackageName() string
}
