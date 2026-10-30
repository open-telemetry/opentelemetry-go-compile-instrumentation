// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

type handler struct {
	fn func()
}

//otelc:span tag:top
func topLevel() {}

//otelc:span tag:var
var pkgVar = func() {}

var (
	//otelc:span tag:spec
	pkgSpec = func(int) {}
)

var pkgValue =
//otelc:span tag:value
func() {}

func run(f func()) { f() }

func newFunc() func() {
	//otelc:span tag:return
	return func() {}
}

func main() {
	topLevel()
	pkgVar()
	pkgSpec(1)
	pkgValue()

	//otelc:span tag:define
	local := func(a, b int) {}
	local(1, 2)

	//otelc:span tag:local-var
	var localVar = func() {}
	localVar()

	//otelc:span tag:assign
	local = func(int, int) {}
	local(3, 4)

	run(
		//otelc:span tag:argument
		func() {},
	)

	//otelc:span tag:go
	go func() {}()

	//otelc:span tag:defer
	defer func() {}()

	//otelc:span tag:call
	func() {}()

	//otelc:span tag:call-result
	result := func() int { return 1 }()
	_ = result

	h := handler{
		//otelc:span tag:field
		fn: func() {},
	}
	h.fn()

	funcs := []func(){
		//otelc:span tag:element
		func() {},
	}
	funcs[0]()

	ch := make(chan func(), 1)
	//otelc:span tag:send
	ch <- func() {}
	(<-ch)()

	newFunc()()
}
