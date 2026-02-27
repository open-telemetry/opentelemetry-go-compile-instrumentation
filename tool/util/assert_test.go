// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAssert_Pass(t *testing.T) {
	Assert(true, "should not fail")
}

func TestAssertType_Pass(t *testing.T) {
	type foo struct{ Name string }
	f := foo{"bar"}

	v := AssertType[foo](f)
	assert.Equal(t, "bar", v.Name)
}

func TestAssertType_Reflection(t *testing.T) {
	d := 123
	var i any = d

	rtype := reflect.TypeOf(AssertType[int](i))
	assert.Equal(t, reflect.TypeOf(123), rtype)
}
