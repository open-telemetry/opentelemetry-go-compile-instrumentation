package main

import "unsafe"

type MyStruct struct {
	Field int
}

func main() {
	s := MyStruct{Field: 42}
	// Test wrapping a call with a complex nested expression
	size := unsafe.Sizeof(s)
	println(size)
}
