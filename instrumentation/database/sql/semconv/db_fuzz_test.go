// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv

import (
	"strings"
	"testing"
	"unicode"
)

// FuzzOperationName asserts that OperationName never panics on arbitrary query
// text, which the db hook passes in unchanged, and that its result is a single
// token that is not a comment marker.
//
// The seed corpus runs as an ordinary table test. Run it as a real fuzzer with:
//
//	go test ./instrumentation/database/sql/semconv/ -run=Fuzz -fuzz=FuzzOperationName -fuzztime=60s
func FuzzOperationName(f *testing.F) {
	for _, seed := range []string{
		"",
		"SELECT * FROM users",
		"-- name: GetAuthor :one\nSELECT id FROM authors WHERE id = $1",
		"-- note\r\ninsert into t values (1)",
		"/* app=shop */ UPDATE users SET x=1",
		"/*/ x */ SELECT 1",
		"/**/",
		"/* SELECT 1",
		"--",
		"-- a\n-- b\n/* c */ -- d\nDELETE FROM t",
		"/*!50001 CREATE TABLE t (a int) */",
		"\xff-- \x00/*",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, query string) {
		op := OperationName(query)
		if strings.IndexFunc(op, unicode.IsSpace) != -1 {
			t.Fatalf("query=%q: OperationName returned %q, which contains whitespace", query, op)
		}
		if strings.HasPrefix(op, "--") || strings.HasPrefix(op, "/*") {
			t.Fatalf("query=%q: OperationName returned %q, which is a comment marker", query, op)
		}
	})
}
