// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestTargets(t *testing.T) {
	for _, got := range []int{Before0(), After0(), Both0()} {
		if got != 1 {
			t.Fatalf("zero params: got %d", got)
		}
	}
	for _, got := range []int{Before3(1, 2, 3), After3(1, 2, 3), Both3(1, 2, 3)} {
		if got != 6 {
			t.Fatalf("three params: got %d", got)
		}
	}
	for _, got := range []int{Before6(1, 2, 3, 4, 5, 6), After6(1, 2, 3, 4, 5, 6), Both6(1, 2, 3, 4, 5, 6)} {
		if got != 21 {
			t.Fatalf("six params: got %d", got)
		}
	}
}
