// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

func Func1(p1 []byte, p2 map[string]any) (chan struct{}, error) {
	println("Hello, World!")
	return nil, nil
}

func main() { Func1([]byte("hello"), nil) }
