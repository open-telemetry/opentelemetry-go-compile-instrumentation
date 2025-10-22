// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

type T struct{}

func (t *T) Func1(p1 string, p2 int) (float32, error) {
	return 0.0, nil
}

func Func1(p1 string, p2 int) (float32, error) {
	println("Hello, World!")
	return 0.0, nil
}

func main() { Func1("hello", 123) }
