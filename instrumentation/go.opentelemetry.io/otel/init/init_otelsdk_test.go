// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package init

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// Under go test, init_otelsdk.go goes into the main package the go command
// generates for the test binary, whose imports are fixed. The file must not
// import anything; it reaches pkg/runtime by linkname.
func TestInitSDKFileAddsNoImports(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "init_otelsdk.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range f.Imports {
		// The parser only accepts valid import path literals.
		path, _ := strconv.Unquote(spec.Path.Value)
		// unsafe is required by //go:linkname and is not a real dependency.
		if path != "unsafe" {
			t.Errorf("init_otelsdk.go imports %q", path)
		}
	}
}
