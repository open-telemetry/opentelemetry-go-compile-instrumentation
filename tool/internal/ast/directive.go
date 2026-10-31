// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"bytes"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dave/dst"
	"go.opentelemetry.io/otelc/tool/ex"
)

// DirectiveArg represents a single key:value argument parsed from a directive comment.
type DirectiveArg struct {
	Key   string
	Value string
}

func directiveCommentBody(dec string) (string, bool) {
	s := strings.TrimSpace(dec)
	if !strings.HasPrefix(s, "//") {
		return "", false
	}
	s = s[2:]
	if len(s) == 0 {
		return "", false
	}
	r, _ := utf8.DecodeRuneInString(s)
	if unicode.IsSpace(r) {
		return "", false
	}
	return s, true
}

// matchDirective checks if a single decoration string matches the given directive.
// The decoration must be a line comment (starting with //) with no space after //,
// and the directive name must follow immediately. If there is text after the directive
// name, it must be separated by whitespace. The bool return alone answers "does this
// decoration match"; the string return is the remainder after the directive name, used
// by callers that also need the directive's arguments.
func matchDirective(dec, directive string) (string, bool) {
	s, ok := directiveCommentBody(dec)
	if !ok {
		return "", false
	}
	if !strings.HasPrefix(s, directive) {
		return "", false
	}
	rest := s[len(directive):]
	if len(rest) == 0 {
		return "", true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	if !unicode.IsSpace(r) {
		return "", false
	}
	return rest, true
}

const otelcDirectivePrefix = "otelc:"

func otelcDirectiveName(dec string) (string, bool) {
	s, ok := directiveCommentBody(dec)
	if !ok || !strings.HasPrefix(s, otelcDirectivePrefix) {
		return "", false
	}
	end := len(s)
	for i, r := range s {
		if unicode.IsSpace(r) {
			end = i
			break
		}
	}
	return s[:end], true
}

func UnknownDirectiveNames(file *dst.File, known map[string]bool) []string {
	seen := make(map[string]bool)
	dst.Inspect(file, func(n dst.Node) bool {
		if n == nil {
			return false
		}
		decs := n.Decorations()
		if decs == nil {
			return true
		}
		collectUnknownDirectives(decs.Start, known, seen)
		collectUnknownDirectives(decs.End, known, seen)
		return true
	})
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func collectUnknownDirectives(decs []string, known, seen map[string]bool) {
	for _, dec := range decs {
		if name, ok := otelcDirectiveName(dec); ok && !known[name] {
			seen[name] = true
		}
	}
}

// parseDirectiveArgs finds the directive in the decoration string, extracts
// the text after the directive name, and parses it into key:value arguments.
func parseDirectiveArgs(dec, directive string) ([]DirectiveArg, error) {
	rest, ok := matchDirective(dec, directive)
	if !ok {
		return nil, ex.Newf("decoration does not match directive %q", directive)
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil, nil
	}
	return scanArgs(rest)
}

// scanArgs parses a string of key:value arguments separated by whitespace.
// Values may be Go double-quoted strings. Single quotes are rejected.
func scanArgs(input string) ([]DirectiveArg, error) {
	tokens, err := tokenize(input)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, nil
	}
	args := make([]DirectiveArg, 0, len(tokens))
	for _, tok := range tokens {
		key, value, found := cutUnquoted(tok, ':')
		if !found {
			return nil, ex.Newf("argument %q has no unquoted colon separator; "+
				"quote only the value, as in key:\"a:b\"", tok)
		}
		if key == "" {
			return nil, ex.Newf("argument %q has an empty key", tok)
		}
		if strings.ContainsRune(key, '"') {
			return nil, ex.Newf("argument %q has a quoted or malformed key", tok)
		}
		if strings.HasPrefix(value, "'") {
			return nil, ex.Newf("single-quoted values are not supported in argument %q", tok)
		}
		if strings.HasPrefix(value, "\"") {
			unquoted, unquoteErr := strconv.Unquote(value)
			if unquoteErr != nil {
				return nil, ex.Wrapf(unquoteErr, "invalid quoted value in argument %q", tok)
			}
			value = unquoted
		}
		args = append(args, DirectiveArg{Key: key, Value: value})
	}
	return args, nil
}

// FileContainsDirectiveText reports whether the raw bytes of the file at path
// hold the literal text "//" + directive anywhere in the file.
func FileContainsDirectiveText(path, directive string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return bytes.Contains(content, []byte("//"+directive)), nil
}

// FileHasDirective reports whether any node decoration in the file matches the
// given directive. The file must have been parsed with ParseFileFast
// or ParseFile so that comment decorations are available.
func FileHasDirective(file *dst.File, directive string) bool {
	found := false
	dst.Inspect(file, func(n dst.Node) bool {
		if found || n == nil {
			return false
		}
		decs := n.Decorations()
		if decs == nil {
			return true
		}
		for _, dec := range decs.Start {
			if _, matched := matchDirective(dec, directive); matched {
				found = true
				return false
			}
		}
		for _, dec := range decs.End {
			if _, matched := matchDirective(dec, directive); matched {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// FuncLeadHasDirective reports whether the function declaration's leading
// decorations contain the given directive. It inspects only the leading
// decorations (the comments directly above the func), not the body or trailing
// decorations, mirroring FileHasLeadingDirective for files.
func FuncLeadHasDirective(funcDecl *dst.FuncDecl, directive string) bool {
	for _, dec := range funcDecl.Decs.Start {
		if _, matched := matchDirective(dec, directive); matched {
			return true
		}
	}
	return false
}

// HasLeadingDirective reports whether the statement's leading decorations
// contain the given directive. A nil stmt has no leading decorations,
// so the function returns false.
func HasLeadingDirective(stmt dst.Stmt, directive string) bool {
	if stmt == nil {
		return false
	}
	decs := stmt.Decorations()
	if decs == nil {
		return false
	}
	for _, dec := range decs.Start {
		if _, matched := matchDirective(dec, directive); matched {
			return true
		}
	}
	return false
}

// FuncDirectiveMatch pairs a function matched by FindFuncsByDirective with
// the key:value arguments parsed from its matching directive comment. Exactly
// one of Func and Lit is set. Enclosing is the top-level declaration that
// contains Lit: a *dst.FuncDecl, or a *dst.GenDecl if Lit is outside any
// function (e.g. in a package-level var).
type FuncDirectiveMatch struct {
	Func      *dst.FuncDecl
	Lit       *dst.FuncLit
	Enclosing dst.Decl
	Args      []DirectiveArg
}

// FileHasLeadingDirective returns true if the file contains a leading run of comments
// before the package clause that matches the given directive.
func FileHasLeadingDirective(file *dst.File, directive string) bool {
	for _, dec := range file.Decs.Start {
		if _, matched := matchDirective(dec, directive); matched {
			return true
		}
	}
	return false
}

// FindFuncsByDirective returns the functions annotated with the specified
// directive comment, in source order, together with the directive's key:value
// arguments for each match. A directive in the leading decorations of a node
// annotates:
//   - a top-level function declaration or a function literal: the node itself.
//   - a var declaration or spec, an assignment, or a return statement: the
//     function literals among its values.
//   - a go, defer, or expression statement: its function literal.
//   - a send statement or a key:value element: its value, if it is a function
//     literal.
//
// A call of a function literal counts as that literal. A literal that more
// than one directive annotates is returned once, with the arguments of the
// first directive.
func FindFuncsByDirective(file *dst.File, directive string) ([]FuncDirectiveMatch, error) {
	var (
		matches   []FuncDirectiveMatch
		enclosing dst.Decl
		err       error
	)
	seen := make(map[*dst.FuncLit]bool)
	visit := func(n dst.Node) bool {
		if err != nil || n == nil {
			return false
		}
		dec, found := leadingDirective(n, directive)
		if !found {
			return true
		}
		if funcDecl, isDecl := n.(*dst.FuncDecl); isDecl {
			args, argsErr := parseDirectiveArgs(dec, directive)
			if argsErr != nil {
				err = ex.Wrapf(argsErr, "parsing directive args for func %s", funcDecl.Name.Name)
				return false
			}
			matches = append(matches, FuncDirectiveMatch{Func: funcDecl, Args: args})
			return true
		}
		var literals []*dst.FuncLit
		for _, lit := range annotatedFuncLiterals(n) {
			if !seen[lit] {
				seen[lit] = true
				literals = append(literals, lit)
			}
		}
		if len(literals) == 0 {
			return true
		}
		args, argsErr := parseDirectiveArgs(dec, directive)
		if argsErr != nil {
			err = ex.Wrapf(argsErr, "parsing directive args for func literal")
			return false
		}
		for _, lit := range literals {
			matches = append(matches, FuncDirectiveMatch{Lit: lit, Enclosing: enclosing, Args: args})
		}
		return true
	}
	for _, decl := range file.Decls {
		enclosing = decl
		dst.Inspect(decl, visit)
	}
	if err != nil {
		return nil, err
	}
	return matches, nil
}

// FindStrayDirectives returns the nodes that carry the specified directive
// comment but that FindFuncsByDirective does not match: a leading directive
// that annotates no function (see FindFuncsByDirective), or a trailing one.
// Only the declarations of the file are searched, so a file-level directive
// before the package clause is not returned.
func FindStrayDirectives(file *dst.File, directive string) []dst.Node {
	var stray []dst.Node
	visit := func(n dst.Node) bool {
		if n == nil {
			return false
		}
		decs := n.Decorations()
		if decs == nil {
			return true
		}
		_, lead := leadingDirective(n, directive)
		_, isDecl := n.(*dst.FuncDecl)
		if lead && !isDecl && len(annotatedFuncLiterals(n)) == 0 {
			stray = append(stray, n)
			return true
		}
		for _, dec := range decs.End {
			if _, matched := matchDirective(dec, directive); matched {
				stray = append(stray, n)
				break
			}
		}
		return true
	}
	for _, decl := range file.Decls {
		dst.Inspect(decl, visit)
	}
	return stray
}

// leadingDirective returns the first leading decoration of n that matches the
// directive.
func leadingDirective(n dst.Node, directive string) (string, bool) {
	decs := n.Decorations()
	if decs == nil {
		return "", false
	}
	for _, dec := range decs.Start {
		if _, matched := matchDirective(dec, directive); matched {
			return dec, true
		}
	}
	return "", false
}

// annotatedFuncLiterals returns the function literals that a directive in the
// leading decorations of n annotates. See FindFuncsByDirective.
func annotatedFuncLiterals(n dst.Node) []*dst.FuncLit {
	switch node := n.(type) {
	case *dst.GenDecl:
		literals := make([]*dst.FuncLit, 0, len(node.Specs))
		for _, spec := range node.Specs {
			literals = append(literals, annotatedFuncLiterals(spec)...)
		}
		return literals
	case *dst.DeclStmt:
		return annotatedFuncLiterals(node.Decl)
	case *dst.ValueSpec:
		return funcLitValues(node.Values...)
	case *dst.AssignStmt:
		return funcLitValues(node.Rhs...)
	case *dst.ReturnStmt:
		return funcLitValues(node.Results...)
	case *dst.GoStmt:
		return funcLitValues(node.Call)
	case *dst.DeferStmt:
		return funcLitValues(node.Call)
	case *dst.ExprStmt:
		return funcLitValues(node.X)
	case *dst.SendStmt:
		return funcLitValues(node.Value)
	case *dst.KeyValueExpr:
		return funcLitValues(node.Value)
	case dst.Expr:
		return funcLitValues(node)
	default:
		return nil
	}
}

// funcLitValues returns the function literals among exprs. A call of a
// function literal counts as that literal. The arguments of a call do not.
// Parentheses around a literal or around the called expression are ignored.
func funcLitValues(exprs ...dst.Expr) []*dst.FuncLit {
	var literals []*dst.FuncLit
	for _, expr := range exprs {
		expr = unparen(expr)
		if call, isCall := expr.(*dst.CallExpr); isCall {
			expr = unparen(call.Fun)
		}
		if lit, isLit := expr.(*dst.FuncLit); isLit {
			literals = append(literals, lit)
		}
	}
	return literals
}

// unparen returns expr without the parentheses around it.
func unparen(expr dst.Expr) dst.Expr {
	for {
		paren, isParen := expr.(*dst.ParenExpr)
		if !isParen {
			return expr
		}
		expr = paren.X
	}
}

// tokenize splits input on whitespace, respecting double-quoted strings.
func tokenize(input string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inQuote := false
	escaped := false

	for _, ch := range input {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' && inQuote {
			current.WriteRune(ch)
			escaped = true
			continue
		}
		if ch == '"' {
			inQuote = !inQuote
			current.WriteRune(ch)
			continue
		}
		if unicode.IsSpace(ch) && !inQuote {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if inQuote {
		return nil, ex.New("unclosed double quote")
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}

// cutUnquoted splits tok at the first occurrence of sep that falls outside
// a double-quoted region, mirroring tokenize's quote/escape tracking so a
// separator inside a quoted key or value is never mistaken for the
// key:value boundary. found is false if no such separator exists.
//
//nolint:revive // confusing-results conflicts with nonamedreturns; see doc comment above for result order
func cutUnquoted(tok string, sep rune) (string, string, bool) {
	inQuote := false
	escaped := false

	for i, ch := range tok {
		switch {
		case escaped:
			escaped = false
		case ch == '\\' && inQuote:
			escaped = true
		case ch == '"':
			inQuote = !inQuote
		case ch == sep && !inQuote:
			return tok[:i], tok[i+utf8.RuneLen(sep):], true
		}
	}
	return tok, "", false
}
