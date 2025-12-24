// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"reflect"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssert_Pass(t *testing.T) {
	Assert(true, "should not fail")
}

func TestAssert_Fail(t *testing.T) {
	const env = "TEST_ASSERT_FAIL"

	if os.Getenv(env) == "1" {
		Assert(false, "should fail")
		return
	}

	code, output := testutil.RunSelfTest(t, "TestAssert_Fail", env)

	require.Equal(t, 1, code, "Assert(false) should exit with code 1")
	assert.Contains(t, output, "Assertion failed: should fail")
}

func TestAssertType_Pass(t *testing.T) {
	type foo struct{ Name string }
	f := foo{"bar"}

	v := AssertType[foo](f)
	assert.Equal(t, "bar", v.Name)
}

func TestAssertType_Fail(t *testing.T) {
	type foo struct{ Name string }
	type bar struct{ Data int }

	const env = "TEST_ASSERT_TYPE_FAIL"

	if os.Getenv(env) == "1" {
		_ = AssertType[bar](foo{"baz"})
		return
	}

	code, output := testutil.RunSelfTest(t, "TestAssertType_Fail", env)

	require.Equal(t, 1, code, "AssertType with wrong type should exit")
	assert.Contains(t, output, "Type assertion failed")
	assert.Contains(t, output, "expected")
}

func TestShouldNotReachHere(t *testing.T) {
	const env = "TEST_SHOULD_NOT_REACH_HERE"

	if os.Getenv(env) == "1" {
		ShouldNotReachHere()
		return
	}

	code, output := testutil.RunSelfTest(t, "TestShouldNotReachHere", env)

	require.Equal(t, 1, code)
	assert.Contains(t, output, "Should not reach here")
}

func TestUnimplemented(t *testing.T) {
	const env = "TEST_UNIMPLEMENTED"

	if os.Getenv(env) == "1" {
		Unimplemented("missing stuff")
		return
	}

	code, output := testutil.RunSelfTest(t, "TestUnimplemented", env)

	require.Equal(t, 1, code)
	assert.Contains(t, output, "Unimplemented: missing stuff")
}

func TestAssertType_Reflection(t *testing.T) {
	d := 123
	var i any = d

	rtype := reflect.TypeOf(AssertType[int](i))
	assert.Equal(t, reflect.TypeOf(123), rtype)
}
