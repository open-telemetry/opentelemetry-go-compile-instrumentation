// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Command gen regenerates public_methods_gen.go and public_methods.otelc.yaml
// from a published github.com/linode/linodego/v2 module version.
//
// Usage (from the instrumentation module root):
//
//	go run ./gen -version v2.4.1
//
// Or via go generate:
//
//	go generate ./...
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	modulePath   = "github.com/linode/linodego/v2"
	instrPath    = "go.opentelemetry.io/otelc/instrumentation/github.com/linode/linodego/v2"
	defaultVer   = "v2.4.1"
	outHooksGo   = "public_methods_gen.go"
	outRulesYAML = "public_methods.otelc.yaml"
)

type method struct {
	name     string
	nParams  int // method params (excluding receiver)
	nResults int
}

func main() {
	version := flag.String("version", defaultVer, "linodego module version to generate against (e.g. v2.4.1)")
	outDir := flag.String("out", ".", "directory for generated files (module root)")
	flag.Parse()

	if !strings.HasPrefix(*version, "v") {
		fatalf("version must look like v2.4.1, got %q", *version)
	}

	modDir := resolveModuleDir(modulePath + "@" + *version)
	methods := collectMethods(modDir)
	if len(methods) == 0 {
		fatalf("no instrumentable *Client methods found in %s", modDir)
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].name < methods[j].name })

	if err := os.WriteFile(filepath.Join(*outDir, outHooksGo), renderHooksGo(*version, methods), 0o644); err != nil {
		fatalf("write %s: %v", outHooksGo, err)
	}
	if err := os.WriteFile(
		filepath.Join(*outDir, outRulesYAML),
		renderRulesYAML(*version, methods),
		0o644,
	); err != nil {
		fatalf("write %s: %v", outRulesYAML, err)
	}
	fmt.Printf("generated %d public Client methods from %s@%s\n", len(methods), modulePath, *version)
}

func resolveModuleDir(modAtVer string) string {
	// Ensure the module is in the cache.
	if out, err := exec.Command("go", "mod", "download", modAtVer).CombinedOutput(); err != nil {
		fatalf("go mod download %s: %v\n%s", modAtVer, err, out)
	}
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", modAtVer).CombinedOutput()
	if err != nil {
		fatalf("go list -m %s: %v\n%s", modAtVer, err, out)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		fatalf("empty module dir for %s", modAtVer)
	}
	return dir
}

func collectMethods(modDir string) []method {
	entries, err := os.ReadDir(modDir)
	if err != nil {
		fatalf("read %s: %v", modDir, err)
	}
	fset := token.NewFileSet()
	var methods []method
	seen := map[string]struct{}{}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(modDir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name == nil || !fn.Name.IsExported() {
				continue
			}
			if !isClientPointerRecv(fn.Recv) {
				continue
			}
			if !hasLeadingContext(fn.Type.Params) || !lastResultIsError(fn.Type.Results) {
				continue
			}
			name := fn.Name.Name
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			methods = append(methods, method{
				name:     name,
				nParams:  fieldCount(fn.Type.Params),
				nResults: fieldCount(fn.Type.Results),
			})
		}
	}
	return methods
}

func isClientPointerRecv(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) != 1 {
		return false
	}
	star, ok := recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	return ok && id.Name == "Client"
}

func hasLeadingContext(params *ast.FieldList) bool {
	if params == nil || len(params.List) == 0 {
		return false
	}
	return typeString(params.List[0].Type) == "context.Context"
}

func lastResultIsError(results *ast.FieldList) bool {
	if results == nil || len(results.List) == 0 {
		return false
	}
	last := results.List[len(results.List)-1]
	return typeString(last.Type) == "error"
}

func fieldCount(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	n := 0
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			n++ // anonymous field
			continue
		}
		n += len(f.Names)
	}
	return n
}

func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name + "." + t.Sel.Name
		}
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.ArrayType:
		return "[]" + typeString(t.Elt)
	case *ast.Ellipsis:
		return "..." + typeString(t.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	}
	return fmt.Sprintf("%T", expr)
}

func renderHooksGo(version string, methods []method) []byte {
	var b bytes.Buffer
	b.WriteString(`// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Code generated from ` + modulePath + `@` + version + `. DO NOT EDIT.
// Unique Before/After wrappers per public Client method so otelc can emit
// one //go:linkname stub per name without package-level redeclarations.
//
// Regenerate with: go run ./gen -version ` + version + `

package v2

import "go.opentelemetry.io/otelc/pkg/hook"

`)
	for _, m := range methods {
		// Before: ictx, recv, p0..p{nParams-1}
		pnames := []string{"recv"}
		for i := 0; i < m.nParams; i++ {
			pnames = append(pnames, fmt.Sprintf("p%d", i))
		}
		beforeParams := []string{"ictx hook.HookContext"}
		for _, p := range pnames {
			beforeParams = append(beforeParams, p+" interface{}")
		}
		fmt.Fprintf(&b, "// Before%s instruments (*Client).%s.\n", m.name, m.name)
		fmt.Fprintf(&b, "func Before%s(%s) {\n", m.name, strings.Join(beforeParams, ", "))
		fmt.Fprintf(&b, "\tbeforeAPICall(ictx, %s)\n", strings.Join(pnames, ", "))
		b.WriteString("}\n\n")

		rnames := make([]string, m.nResults)
		afterParams := []string{"ictx hook.HookContext"}
		for i := 0; i < m.nResults; i++ {
			rnames[i] = fmt.Sprintf("r%d", i)
			afterParams = append(afterParams, rnames[i]+" interface{}")
		}
		fmt.Fprintf(&b, "// After%s finishes the span for (*Client).%s.\n", m.name, m.name)
		fmt.Fprintf(&b, "func After%s(%s) {\n", m.name, strings.Join(afterParams, ", "))
		fmt.Fprintf(&b, "\tafterAPICall(ictx, errorFromResults(%s))\n", strings.Join(rnames, ", "))
		b.WriteString("}\n\n")
	}
	return b.Bytes()
}

func renderRulesYAML(version string, methods []method) []byte {
	var b bytes.Buffer
	b.WriteString(`# Code generated from ` + modulePath + `@` + version + `.
# One rule per public *Client method (context + error). Unique hook names avoid
# //go:linkname redeclaration across package files.
#
# version is a minimum bound (see tool/util.VersionInRange): rules apply to
# linodego ` + version + ` and newer releases on the v2 module path.
# Regenerate with: go run ./gen -version ` + version + `
# Do not edit by hand.

`)
	for _, m := range methods {
		fmt.Fprintf(&b, "linodego_api_%s:\n", m.name)
		b.WriteString("  target: github.com/linode/linodego/v2\n")
		fmt.Fprintf(&b, "  version: %s\n", version)
		b.WriteString("  where:\n")
		fmt.Fprintf(&b, "    func: %s\n", m.name)
		b.WriteString("    recv: \"*Client\"\n")
		b.WriteString("  do:\n")
		b.WriteString("    - inject_hooks:\n")
		fmt.Fprintf(&b, "        before: Before%s\n", m.name)
		fmt.Fprintf(&b, "        after: After%s\n", m.name)
		fmt.Fprintf(&b, "        path: %q\n", instrPath)
		b.WriteString("\n")
	}
	return b.Bytes()
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
