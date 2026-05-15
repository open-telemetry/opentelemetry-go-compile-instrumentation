// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

func GenericFunc[T any](p1 T, p2 int) (T, error) {
	return p1, nil
}

type GenStruct[T any] struct {
	value T
}

func (g *GenStruct[T]) GenericMethod(p1 T, p2 string) (T, error) {
	return p1, nil
}
