// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
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

// matchDirective checks if a single decoration string matches the given directive.
// The decoration must be a line comment (starting with //) with no space after //,
// and the directive name must follow immediately. If there is text after the directive
// name, it must be separated by whitespace. The bool return alone answers "does this
// decoration match"; the string return is the remainder after the directive name, used
// by callers that also need the directive's arguments.
func matchDirective(dec, directive string) (string, bool) {
	s := strings.TrimSpace(dec)
	if !strings.HasPrefix(s, "//") {
		return "", false
	}
	s = s[2:] // strip "//"
	// No space allowed immediately after "//"
	if len(s) == 0 {
		return "", false
	}
	r, _ := utf8.DecodeRuneInString(s)
	if unicode.IsSpace(r) {
		return "", false
	}
	// Check directive name matches
	if !strings.HasPrefix(s, directive) {
		return "", false
	}
	rest := s[len(directive):]
	if len(rest) == 0 {
		return "", true
	}
	// Next character after directive must be whitespace (not another identifier char)
	r, _ = utf8.DecodeRuneInString(rest)
	if !unicode.IsSpace(r) {
		return "", false
	}
	return rest, true
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
		key, value, found := strings.Cut(tok, ":")
		if !found {
			return nil, ex.Newf("argument %q missing colon separator", tok)
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

// FuncDirectiveMatch pairs a function declaration matched by
// FindFuncsByDirective with the key:value arguments parsed from its matching
// directive comment
type FuncDirectiveMatch struct {
	Func *dst.FuncDecl
	Args []DirectiveArg
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

// FindFuncsByDirective returns all top-level function declarations whose
// leading decorations contain the specified directive comment, together with
// the directive's key:value arguments for each match.
func FindFuncsByDirective(file *dst.File, directive string) ([]FuncDirectiveMatch, error) {
	var matches []FuncDirectiveMatch
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}
		for _, dec := range funcDecl.Decs.Start {
			if _, matched := matchDirective(dec, directive); !matched {
				continue
			}
			args, err := parseDirectiveArgs(dec, directive)
			if err != nil {
				return nil, ex.Wrapf(err, "parsing directive args for func %s", funcDecl.Name.Name)
			}
			matches = append(matches, FuncDirectiveMatch{Func: funcDecl, Args: args})
			break
		}
	}
	return matches, nil
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
