// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"strings"
	"testing"
)

// FuzzScanArgs asserts two invariants for every input:
//
//  1. scanArgs never panics.
//  2. Every key in a successful parse is non-empty and contains no double
//     quotes, upholding the postcondition that cutUnquoted and tokenize agree
//     on quote boundaries.
//
// The seed corpus mirrors the TestScanArgs table so that these cases run as
// ordinary unit tests in CI at no extra cost. Run as a real fuzzer with, for
// example:
//
//	go test ./tool/internal/ast/ -run=Fuzz -fuzz=FuzzScanArgs -fuzztime=60s
func FuzzScanArgs(f *testing.F) {
	seeds := []string{
		// Valid inputs from TestScanArgs.
		"key:value",
		`span.name:"my operation" tag:simple`,
		`key:"hello\nworld"`,
		"  key1:v1   key2:v2  ",
		"key:",
		`url:"https://example.com/path"`,
		"key:a:b:c",
		`op:"http:post" tag:foo`,
		`k\:v`,
		// Error inputs from TestScanArgs — scanArgs must not panic on these.
		"key:'single'",
		`key:"unclosed`,
		"",
		"nocolon",
		":value",
		":",
		`"key:value"`,
		`"k":v`,
		`"a\"b":value`,
		// Additional boundary shapes.
		`"" `,
		`key:""`,
		`key:"a"b`,
		strings.Repeat(":", 8),
		strings.Repeat(`"`, 4),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		args, err := scanArgs(input)
		if err != nil {
			// Error path: no invariants to check beyond no-panic.
			return
		}
		// Success path: every returned key must be non-empty and quote-free.
		// A violation means cutUnquoted let a quoted region bleed into the key,
		// which is the cross-component invariant between tokenize and cutUnquoted.
		for _, a := range args {
			if a.Key == "" {
				t.Fatalf("scanArgs(%q): got arg with empty key", input)
			}
			if strings.ContainsRune(a.Key, '"') {
				t.Fatalf("scanArgs(%q): got key %q containing a double quote", input, a.Key)
			}
		}
	})
}
