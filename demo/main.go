// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"runtime"
)

type traceContext struct {
	traceID string
	spanID  string
}

func (tc *traceContext) String() string {
	return fmt.Sprintf("traceID: %s, spanID: %s", tc.traceID, tc.spanID)
}

func (tc *traceContext) Clone() interface{} {
	return &traceContext{
		traceID: tc.traceID,
		spanID:  tc.spanID,
	}
}

type MyStruct struct{}

func (m *MyStruct) Example() { println("MyStruct.Example") }

// Example demonstrates how to use the instrumenter.
func Example() {
	// Output:
	// [MyHook] start to instrument hello world!
	// [MyHook] hello world is instrumented!
}

func main() {
	context := &traceContext{
		traceID: "123",
		spanID:  "456",
	}
	runtime.SetTraceContextToGLS(context)

	go func() {
		fmt.Printf("traceContext from parent goroutine: %s\n", runtime.GetTraceContextFromGLS())
	}()

	// Call the Example function to trigger the instrumentation
	Example()
	m := &MyStruct{}
	// Add a new field to the struct
	println(m.NewField)
	m.Example()
}
