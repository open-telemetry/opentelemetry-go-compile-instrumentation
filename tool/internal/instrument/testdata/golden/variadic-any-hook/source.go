// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

type options struct{ name string }

func New(id int, opts ...func(*options)) string {
	o := options{}
	for _, fn := range opts {
		fn(&o)
	}
	return o.name
}

func main() {}
