// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"testing"
)

var sink int

//go:noinline
func Before0() int { return 1 }

//go:noinline
func After0() int { return 1 }

//go:noinline
func Both0() int { return 1 }

//go:noinline
func Before3(p0 int, p1 int, p2 int) int { return p0 + p1 + p2 }

//go:noinline
func After3(p0 int, p1 int, p2 int) int { return p0 + p1 + p2 }

//go:noinline
func Both3(p0 int, p1 int, p2 int) int { return p0 + p1 + p2 }

//go:noinline
func Before6(p0 int, p1 int, p2 int, p3 int, p4 int, p5 int) int { return p0 + p1 + p2 + p3 + p4 + p5 }

//go:noinline
func After6(p0 int, p1 int, p2 int, p3 int, p4 int, p5 int) int { return p0 + p1 + p2 + p3 + p4 + p5 }

//go:noinline
func Both6(p0 int, p1 int, p2 int, p3 int, p4 int, p5 int) int { return p0 + p1 + p2 + p3 + p4 + p5 }

func main() {
	testing.Init()
	flag.Parse()
	for _, bench := range []struct {
		name string
		run  func(*testing.B)
	}{
		{"Before0", func(b *testing.B) {
			for b.Loop() {
				sink = Before0()
			}
		}},
		{"After0", func(b *testing.B) {
			for b.Loop() {
				sink = After0()
			}
		}},
		{"Both0", func(b *testing.B) {
			for b.Loop() {
				sink = Both0()
			}
		}},
		{"Before3", func(b *testing.B) {
			for b.Loop() {
				sink = Before3(1, 2, 3)
			}
		}},
		{"After3", func(b *testing.B) {
			for b.Loop() {
				sink = After3(1, 2, 3)
			}
		}},
		{"Both3", func(b *testing.B) {
			for b.Loop() {
				sink = Both3(1, 2, 3)
			}
		}},
		{"Before6", func(b *testing.B) {
			for b.Loop() {
				sink = Before6(1, 2, 3, 4, 5, 6)
			}
		}},
		{"After6", func(b *testing.B) {
			for b.Loop() {
				sink = After6(1, 2, 3, 4, 5, 6)
			}
		}},
		{"Both6", func(b *testing.B) {
			for b.Loop() {
				sink = Both6(1, 2, 3, 4, 5, 6)
			}
		}},
	} {
		result := testing.Benchmark(bench.run)
		fmt.Printf("Benchmark%s %s\t%s\n", bench.name, result.String(), result.MemString())
	}
}
