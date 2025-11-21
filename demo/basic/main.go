// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"runtime"
	"time"

	"golang.org/x/time/rate"
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

type GenStruct[T any] struct {
	Value T
}

func (m *GenStruct[T]) GenericRecvExample(t T) T {
	fmt.Printf("%s%s\n", m.Value, t)
	return t
}

func GenericExample[K comparable, V any](key K, value V) V {
	println("Hello, Generic World!", key, value)
	return value
}

// Example demonstrates how to use the instrumenter.
func Example() {
	// Output:
	// [MyHook] start to instrument hello world!
	// [MyHook] hello world is instrumented!
}

func Underscore(_ int, _ float32) {}

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
	m.NewField = "abc"
	m.Example()

	_ = GenericExample(1, 2)
	g := &GenStruct[string]{Value: "Hello"}
	_ = g.GenericRecvExample(", Generic Recv World!")

	// Call real module function
	println(rate.Every(time.Duration(1)))
}
