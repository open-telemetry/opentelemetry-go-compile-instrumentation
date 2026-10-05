// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

type Calculator struct{}

func (c *Calculator) Divide(a int, b int) (int, error) {
	return a / b, nil
}

func main() { (&Calculator{}).Divide(4, 2) }
