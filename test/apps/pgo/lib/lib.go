// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

func Work() int {
	s := 0
	for i := 0; i < 10000; i++ {
		s += i
	}
	return s
}
