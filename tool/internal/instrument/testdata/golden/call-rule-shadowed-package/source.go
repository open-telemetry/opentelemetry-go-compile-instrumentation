// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "strings"

type loud struct{}

func (loud) ToUpper(s string) string { return s + "!" }

func Wrapper(s string) string {
	println("Wrapped!")
	return s
}

func Package(s string) string {
	return strings.ToUpper(s)
}

func Param(strings loud) string {
	return strings.ToUpper("param")
}

func Local() string {
	strings := loud{}
	return strings.ToUpper("local")
}

func Scoped() string {
	{
		strings := loud{}
		_ = strings.ToUpper("inner")
	}
	return strings.ToUpper("outer")
}

func main() {
	_ = Package("a")
	_ = Param(loud{})
	_ = Local()
	_ = Scoped()
}
