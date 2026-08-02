// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// genHookContext mirrors the HookContextImpl the trampoline generates for a
// concrete *int param and *error return (see setValue/getValue): Set writes
// through the stored pointer, using the zero value when val is nil. The real
// type is synthesized per target function and can't be imported, so we stand it
// in here to run the nil path that the golden suite only compiles, never runs.
type genHookContext struct {
	params     []any
	returnVals []any
}

func (c *genHookContext) SetParam(idx int, val any) {
	if idx == 0 {
		if val == nil {
			*(c.params[0].(*int)) = 0
		} else {
			*(c.params[0].(*int)) = val.(int)
		}
	}
}

func (c *genHookContext) GetParam(idx int) any {
	if idx == 0 {
		return *(c.params[0].(*int))
	}
	return nil
}

func (c *genHookContext) SetReturnVal(idx int, val any) {
	if idx == 0 {
		if val == nil {
			*(c.returnVals[0].(*error)) = nil
		} else {
			*(c.returnVals[0].(*error)) = val.(error)
		}
	}
}

func (c *genHookContext) GetReturnVal(idx int) any {
	if idx == 0 {
		return *(c.returnVals[0].(*error))
	}
	return nil
}

// TestHookContextSetNilWritesZeroValue is the runtime guard for #726: before the
// fix, Set*(idx, nil) replaced the slot with nil, so the next Get* panicked on
// the type assertion and the underlying value never changed. A nil val must now
// write the concrete type's zero value through the stored pointer.
func TestHookContextSetNilWritesZeroValue(t *testing.T) {
	param := 42
	retVal := errors.New("boom")
	c := &genHookContext{
		params:     []any{&param},
		returnVals: []any{&retVal},
	}

	require.NotPanics(t, func() { c.SetParam(0, nil) })
	assert.Equal(t, 0, c.GetParam(0))
	assert.Equal(t, 0, param) // written through the pointer, not just the slot

	require.NotPanics(t, func() { c.SetReturnVal(0, nil) })
	assert.Nil(t, c.GetReturnVal(0))
	require.NoError(t, retVal) // the error itself was zeroed to nil

	c.SetParam(0, 7)
	assert.Equal(t, 7, c.GetParam(0))
}
