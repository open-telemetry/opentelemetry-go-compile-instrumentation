// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ex

import (
	"errors"
	"os"
	"regexp"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAndErrorMessage(t *testing.T) {
	err := New("something went wrong")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "something went wrong")
}

func TestNewf(t *testing.T) {
	err := Newf("error code: %d", 77)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error code: 77")
}

func TestWrapAndWrapf(t *testing.T) {
	baseErr := errors.New("root cause")

	wrapped := Wrap(baseErr)
	require.Error(t, wrapped)
	assert.Contains(t, wrapped.Error(), "root cause")

	wrappedf := Wrapf(baseErr, "code: %d", 77)
	require.Error(t, wrappedf)
	assert.Contains(t, wrappedf.Error(), "code: 77: root cause")
}

func TestUnwrap(t *testing.T) {
	baseErr := errors.New("root cause")
	w1 := Wrap(baseErr)
	w2 := Wrap(w1)

	assert.True(t, errors.Is(w2, baseErr))
}

func TestStackTracePresent(t *testing.T) {
	err := New("root cause")

	var se *stackfulError
	require.True(t, errors.As(err, &se))
	require.NotEmpty(t, se.frame)

	pattern := regexp.MustCompile(`^\[[0-9]+].+:[0-9]+ .*`)
	for _, fr := range se.frame {
		require.Regexp(t, pattern, fr, "invalid stack frame format")
	}
}

func TestFatalf_StackfulExit(t *testing.T) {
	const env = "TEST_FATALF_STACKFUL"

	if os.Getenv(env) == "1" {
		Fatalf("should fail: %v", 77)
		return
	}

	code, output := testutil.RunSelfTest(t, "TestFatalf_StackfulExit", env)

	require.Equal(t, 1, code, "Fatalf should exit with code 1")
	assert.Contains(t, output, "should fail: 77")
	assert.Contains(t, output, "Stack:")
}

func TestFatal_NonStackfulPanics(t *testing.T) {
	const env = "TEST_FATAL_PANIC"

	if os.Getenv(env) == "1" {
		Fatal(errors.New("no stackful"))
		return
	}

	code, _ := testutil.RunSelfTest(t, "TestFatal_NonStackfulPanics", env)

	require.Equal(t, 2, code, "Fatal(non-stackful) should panic")
}

func TestFatal_NilErrorPanics(t *testing.T) {
	const env = "TEST_FATAL_EMPTY"

	if os.Getenv(env) == "1" {
		Fatal(nil)
		return
	}

	code, _ := testutil.RunSelfTest(t, "TestFatal_NilErrorPanics", env)

	require.Equal(t, 2, code, "Fatal(nil) should panic")
}
