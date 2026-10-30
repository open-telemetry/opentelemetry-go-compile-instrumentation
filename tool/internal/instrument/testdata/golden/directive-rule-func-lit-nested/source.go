// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

//otelc:span tag:outer-decl
func outer() {
	//otelc:span tag:inner
	inner := func() {
		plain := func() {}
		plain()
	}
	inner()
}

func main() {
	outer()

	//otelc:span tag:outer-lit
	outerLit := func() {
		//otelc:span tag:inner-lit
		innerLit := func() {}
		innerLit()
	}
	outerLit()

	//otelc:span tag:statement
	twice :=
		//otelc:span tag:literal
		func() {}
	twice()
}
