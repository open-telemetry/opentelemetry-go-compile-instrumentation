// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package generic

// Box stands in for a generic struct of a third-party package.
type Box[T any] struct {
	Value T
	Tag   string
}

// Pair has two type parameters.
type Pair[K comparable, V any] struct {
	Key K
	Val V
	Tag string
}
