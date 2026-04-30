// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

func func1() {
	println(", World!")
	println(", World!") // duplicate match case
}

func func2() {
	go func() {
		println(", World!") // inside goroutine
	}()
	println(", World!") // outside goroutine
}

func func3() {
	ch := make(chan int)

	select {
	case <-ch:
		println("Hello, World!")
	}
}

func func4() {
	if true {
		println("IF BLOCK")
	}
}

func func5() {
	for i := 0; i < 3; i++ {
		println("loop")
	}
}

func func6() {
	fmt.Println(
		"multiline",
	)
}

func func7() {
	println("exact")
	println("exact ")
}
