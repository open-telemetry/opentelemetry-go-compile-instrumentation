// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

type Set[T comparable] struct {
	items map[T]struct{}
}

func (s *Set[_]) Len() int {
	return len(s.items)
}

type Pair[K comparable, V any] struct {
	key   K
	value V
}

func (p Pair[K, _]) Key() K {
	return p.key
}
