// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssertType_Success(t *testing.T) {
	// Concrete type
	val := AssertType[int](123)
	assert.Equal(t, 123, val)

	// Pointer type
	s := "hello"
	ps := AssertType[*string](&s)
	assert.Equal(t, &s, ps)
	assert.Equal(t, "hello", *ps)
}

func TestAssertType_NilPointer(t *testing.T) {
	var s *string

	ps := AssertType[*string](s)
	assert.Nil(t, ps)
}

func TestAssertType_NilFailure(t *testing.T) {
	if os.Getenv("ASSERTTYPE_FATAL") == "1" {
		AssertType[*string](nil)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestAssertType_NilFailure")
	cmd.Env = append(os.Environ(), "ASSERTTYPE_FATAL=1")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)

	assert.Contains(t,
		stderr.String(),
		"Type assertion failed: got nil, expected *string")
}

func TestAssertType_InvalidType(t *testing.T) {
	if os.Getenv("ASSERTTYPE_INVALID") == "1" {
		AssertType[string](123)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestAssertType_InvalidType")
	cmd.Env = append(os.Environ(), "ASSERTTYPE_INVALID=1")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)

	assert.Contains(t,
		stderr.String(),
		"Type assertion failed: got int, expected string")
}

func TestAssertPasses(t *testing.T) {
	// A satisfied assertion must not exit the process.
	assert.NotPanics(t, func() {
		Assert(true, "should not fail")
	})
}

func TestAssertType(t *testing.T) {
	// A matching type assertion returns the typed value.
	var v any = "hello"
	assert.Equal(t, "hello", AssertType[string](v))
}

// fatalCaseVar names the environment variable that tells a re-executed test
// binary which fatal path to run. The parent never sets it, so the guard in
// each test below is skipped during an ordinary run.
const fatalCaseVar = "UTIL_FATAL_CASE"

// runFatalCase re-execs this test binary with only the named test selected and
// the guard set, so the branch that calls ex.Fatalf runs in a child process
// rather than taking the test run down with it. It returns the child's
// combined output and the exit error, which is always non-nil for these paths.
func runFatalCase(t *testing.T, testName, caseName string) (string, error) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), fatalCaseVar+"="+caseName)
	out, err := cmd.CombinedOutput()

	return string(out), err
}

// requireNonZeroExit asserts the child died the way a fatal path should, and
// reports the child's output when it did not so a failure is diagnosable.
func requireNonZeroExit(t *testing.T, out string, err error) {
	t.Helper()

	var exitErr *exec.ExitError
	require.ErrorAsf(t, err, &exitErr,
		"expected a non-zero exit from the fatal path; child output:\n%s", out)
	assert.NotZero(t, exitErr.ExitCode(), "a fatal path should not exit successfully")
}

// The satisfied branch of Assert is already covered by TestAssertPasses above,
// so only the failing branch is added here.
func TestAssertFatalWhenConditionIsFalse(t *testing.T) {
	if os.Getenv(fatalCaseVar) == "assert" {
		Assert(false, "receiver must not be nil")
		return
	}

	out, err := runFatalCase(t, "TestAssertFatalWhenConditionIsFalse", "assert")

	requireNonZeroExit(t, out, err)
	assert.Contains(t, out, "Assertion failed: receiver must not be nil",
		"the failing assertion should report the message it was given")
}

func TestShouldNotReachHereIsFatal(t *testing.T) {
	if os.Getenv(fatalCaseVar) == "unreachable" {
		ShouldNotReachHere()
		return
	}

	out, err := runFatalCase(t, "TestShouldNotReachHereIsFatal", "unreachable")

	requireNonZeroExit(t, out, err)
	assert.Contains(t, out, "Should not reach here!")
}

func TestUnimplementedIsFatal(t *testing.T) {
	if os.Getenv(fatalCaseVar) == "unimplemented" {
		Unimplemented("generic type parameters on embedded fields")
		return
	}

	out, err := runFatalCase(t, "TestUnimplementedIsFatal", "unimplemented")

	requireNonZeroExit(t, out, err)
	assert.Contains(t, out, "Unimplemented: generic type parameters on embedded fields",
		"the message should identify what is missing, not just that something is")
}
