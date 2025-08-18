// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ex

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

const numSkipFrame = 3 // skip the Errorf/Fatalf caller

// stackfulError represents an error with stack trace information
type stackfulError struct {
	message []string
	frame   []string
	wrapped error
}

func (e *stackfulError) Error() string { return strings.Join(e.message, "\n") }
func (e *stackfulError) Unwrap() error { return e.wrapped }

func getFrames() []string {
	const initFrames = 30
	frameList := make([]string, 0)
	pcs := make([]uintptr, initFrames)
	n := runtime.Callers(numSkipFrame, pcs)
	if n == 0 {
		return frameList
	}
	pcs = pcs[:n]
	frames := runtime.CallersFrames(pcs)
	cnt := 0
	for {
		frame, more := frames.Next()
		if !more {
			break
		}
		const prefix = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation"
		fnName := strings.TrimPrefix(frame.Function, prefix)
		f := fmt.Sprintf("[%d]%s:%d %s", cnt, frame.File, frame.Line, fnName)
		frameList = append(frameList, f)
		cnt++
	}
	return frameList
}

func Error(previousErr error) error {
	return Errorf(previousErr, "")
}

// Errorf wraps an error with stack trace information and a formatted message
// If you want to decorate the existing error, use it.
func Errorf(previousErr error, format string, args ...any) error {
	se := &stackfulError{}
	if errors.As(previousErr, &se) {
		se.message = append(se.message, fmt.Sprintf(format, args...))
		return previousErr
	}
	// User defined error message + existing error message
	errMsg := fmt.Sprintf(format, args...)
	if previousErr != nil {
		errMsg = fmt.Sprintf("%s: %s", errMsg, previousErr.Error())
	}
	e := &stackfulError{
		message: []string{errMsg},
		frame:   getFrames(),
		wrapped: previousErr,
	}
	return e
}

func Fatalf(format string, args ...any) { Fatal(Errorf(nil, format, args...)) }

func Fatal(err error) {
	if err == nil {
		panic("Fatal error: unknown")
	}
	e := &stackfulError{}
	if errors.As(err, &e) {
		em := ""
		for i, m := range e.message {
			em += fmt.Sprintf("[%d] %s\n", i, m)
		}
		msg := fmt.Sprintf("Error:\n%s\nStack:\n%s", em, strings.Join(e.frame, "\n"))
		_, _ = fmt.Fprint(os.Stderr, msg)
		os.Exit(1)
	}
	panic(err)
}
