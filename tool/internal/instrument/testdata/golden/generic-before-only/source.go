// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

func GenericFuncBeforeOnly[T any](p1 T, p2 int) (T, error) {
	return p1, nil
}
