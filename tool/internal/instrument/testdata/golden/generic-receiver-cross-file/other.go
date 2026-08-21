// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

type GenStruct[T fmt.Stringer] struct {
	value T
}
