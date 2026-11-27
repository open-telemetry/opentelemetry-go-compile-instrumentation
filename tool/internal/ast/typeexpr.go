// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"strings"

	"github.com/dave/dst"
)

// typeKind tells the shape of a parsed composite type-name filter.
type typeKind int

const (
	kindNamed typeKind = iota
	kindPointer
	kindSlice
	kindArray
	kindVariadic
	kindMap
	kindChan
	kindFunc
	kindEmptyStruct
)

// typeExpr is a type-name filter that is not a plain [*][pkg.]Name, such as
// []byte, map[string]any, chan struct{} or func(context.Context) error. It
// mirrors the shape of the Go type expression it has to match.
type typeExpr struct {
	kind typeKind

	// importPath and name hold a kindNamed leaf.
	importPath string
	name       string

	// elem is the element of a pointer, slice, array, variadic or chan, and the
	// value of a map. key is the key of a map.
	elem *typeExpr
	key  *typeExpr

	// length is the source text of a kindArray length, such as 4 or N.
	length string

	// dir is the direction of a kindChan.
	dir dst.ChanDir

	// params and results hold the types of a kindFunc, in order.
	params  []*typeExpr
	results []*typeExpr
}

// maxTypeExprDepth bounds the nesting of a type-name filter, so that a
// pathological string cannot recurse without limit.
const maxTypeExprDepth = 64

const (
	keywordChan = "chan"
	keywordFunc = "func"
	nameAny     = "any"
)

// typeExprParser reads the type-name filter syntax:
//
//	Type     = Named | "*" Type | "[" "]" Type | "[" Length "]" Type |
//	           "..." Type | "map" "[" Type "]" Type | ChanType |
//	           "func" "(" [ Type { "," Type } ] ")" [ Result ] |
//	           "struct{}" | "interface{}" .
//	ChanType = "chan" Type | "chan<-" Type | "<-chan" Type .
//	Result   = Type | "(" [ Type { "," Type } ] ")" .
//
// Named is a type name as the plain filters write it: an optional import path
// and a dot, then the name, as in net/http.Request. The empty interface reads
// as any, and parameter names in a func type are not accepted.
type typeExprParser struct {
	src   string
	pos   int
	depth int
}

// parseTypeExpr parses src. It reports false when src is not a type-name
// filter of this syntax.
func parseTypeExpr(src string) (*typeExpr, bool) {
	p := &typeExprParser{src: src}
	expr, ok := p.parseType()
	if !ok {
		return nil, false
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return nil, false
	}
	return expr, true
}

func (p *typeExprParser) skipSpace() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

// consume reads token when the source continues with it, after any space.
func (p *typeExprParser) consume(token string) bool {
	p.skipSpace()
	if !strings.HasPrefix(p.src[p.pos:], token) {
		return false
	}
	p.pos += len(token)
	return true
}

// peek reports the next byte after any space, or 0 at the end of the source.
func (p *typeExprParser) peek() byte {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

// isTypeKeyword reports whether name starts a composite type, so that it is not
// a type name of its own.
func isTypeKeyword(name string) bool {
	switch name {
	case keywordChan, keywordFunc, "interface", "map", "struct":
		return true
	}
	return false
}

func isWordByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	default:
		return c == '_' || c == '.' || c == '-' || c == '/'
	}
}

// word reads the longest run of bytes a type name or keyword is made of.
func (p *typeExprParser) word() string {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.src) && isWordByte(p.src[p.pos]) {
		p.pos++
	}
	return p.src[start:p.pos]
}

func (p *typeExprParser) parseType() (*typeExpr, bool) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxTypeExprDepth {
		return nil, false
	}

	switch {
	case p.consume("*"):
		return p.wrap(kindPointer)
	case p.consume("..."):
		return p.wrap(kindVariadic)
	case p.consume("<-"):
		if p.word() != keywordChan {
			return nil, false
		}
		return p.parseChanElem(dst.RECV)
	case p.consume("["):
		return p.parseBracketed()
	}
	return p.parseWord(p.word())
}

// wrap parses the element type that follows a prefix operator.
func (p *typeExprParser) wrap(kind typeKind) (*typeExpr, bool) {
	elem, ok := p.parseType()
	if !ok {
		return nil, false
	}
	return &typeExpr{kind: kind, elem: elem}, true
}

// parseBracketed parses what follows a "[": a slice or an array type.
func (p *typeExprParser) parseBracketed() (*typeExpr, bool) {
	length := p.word()
	if !p.consume("]") {
		return nil, false
	}
	elem, ok := p.parseType()
	if !ok {
		return nil, false
	}
	if length == "" {
		return &typeExpr{kind: kindSlice, elem: elem}, true
	}
	if !isArrayLength(length) {
		return nil, false
	}
	return &typeExpr{kind: kindArray, length: length, elem: elem}, true
}

// isArrayLength reports whether s is a decimal literal or an identifier.
func isArrayLength(s string) bool {
	digits := s[0] >= '0' && s[0] <= '9'
	for _, c := range []byte(s) {
		isDigit := c >= '0' && c <= '9'
		isLetter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
		if digits && !isDigit || !digits && !isDigit && !isLetter {
			return false
		}
	}
	return true
}

// parseWord parses the type that starts with w, a keyword or a type name.
func (p *typeExprParser) parseWord(w string) (*typeExpr, bool) {
	switch w {
	case "":
		return nil, false
	case "map":
		return p.parseMap()
	case keywordChan:
		if p.consume("<-") {
			return p.parseChanElem(dst.SEND)
		}
		return p.parseChanElem(dst.SEND | dst.RECV)
	case keywordFunc:
		return p.parseFunc()
	case "struct":
		if p.consume("{") && p.consume("}") {
			return &typeExpr{kind: kindEmptyStruct}, true
		}
		return nil, false
	case "interface":
		if p.consume("{") && p.consume("}") {
			return &typeExpr{kind: kindNamed, name: nameAny}, true
		}
		return nil, false
	}
	m := typeNameRe.FindStringSubmatch(w)
	if m == nil || (m[2] == "" && isTypeKeyword(m[3])) || strings.HasPrefix(m[2], ".") {
		return nil, false
	}
	return &typeExpr{kind: kindNamed, importPath: m[2], name: m[3]}, true
}

func (p *typeExprParser) parseChanElem(dir dst.ChanDir) (*typeExpr, bool) {
	elem, ok := p.parseType()
	if !ok {
		return nil, false
	}
	return &typeExpr{kind: kindChan, dir: dir, elem: elem}, true
}

func (p *typeExprParser) parseMap() (*typeExpr, bool) {
	if !p.consume("[") {
		return nil, false
	}
	key, ok := p.parseType()
	if !ok || !p.consume("]") {
		return nil, false
	}
	value, ok := p.parseType()
	if !ok {
		return nil, false
	}
	return &typeExpr{kind: kindMap, key: key, elem: value}, true
}

func (p *typeExprParser) parseFunc() (*typeExpr, bool) {
	params, ok := p.parseTypeList()
	if !ok {
		return nil, false
	}
	fn := &typeExpr{kind: kindFunc, params: params}
	switch p.peek() {
	case 0, ',', ')', ']':
		return fn, true
	case '(':
		fn.results, ok = p.parseTypeList()
		return fn, ok
	}
	result, ok := p.parseType()
	if !ok {
		return nil, false
	}
	fn.results = []*typeExpr{result}
	return fn, true
}

// parseTypeList parses a parenthesized, comma separated list of types. A
// trailing comma is accepted.
func (p *typeExprParser) parseTypeList() ([]*typeExpr, bool) {
	if !p.consume("(") {
		return nil, false
	}
	var list []*typeExpr
	for !p.consume(")") {
		item, ok := p.parseType()
		if !ok {
			return nil, false
		}
		list = append(list, item)
		if !p.consume(",") && p.peek() != ')' {
			return nil, false
		}
	}
	return list, true
}

// matches reports whether node has the type the filter describes. Every part
// of the type has to match: []string does not match []int, and map[string]any
// does not match map[string]string.
func (e *typeExpr) matches(node dst.Expr, imports map[string]string) bool {
	if paren, ok := node.(*dst.ParenExpr); ok {
		return e.matches(paren.X, imports)
	}
	switch e.kind {
	case kindNamed:
		return parsedTypeName{importPath: e.importPath, name: e.name}.matches(node, imports)
	case kindPointer:
		star, ok := node.(*dst.StarExpr)
		return ok && e.elem.matches(star.X, imports)
	case kindSlice:
		arr, ok := node.(*dst.ArrayType)
		return ok && arr.Len == nil && e.elem.matches(arr.Elt, imports)
	case kindArray:
		arr, ok := node.(*dst.ArrayType)
		return ok && arr.Len != nil && arrayLengthText(arr.Len) == e.length && e.elem.matches(arr.Elt, imports)
	case kindVariadic:
		ell, ok := node.(*dst.Ellipsis)
		return ok && ell.Elt != nil && e.elem.matches(ell.Elt, imports)
	case kindMap:
		m, ok := node.(*dst.MapType)
		return ok && e.key.matches(m.Key, imports) && e.elem.matches(m.Value, imports)
	case kindChan:
		ch, ok := node.(*dst.ChanType)
		return ok && ch.Dir == e.dir && e.elem.matches(ch.Value, imports)
	case kindFunc:
		fn, ok := node.(*dst.FuncType)
		return ok && e.matchesFunc(fn, imports)
	case kindEmptyStruct:
		st, ok := node.(*dst.StructType)
		return ok && (st.Fields == nil || len(st.Fields.List) == 0)
	}
	return false
}

// matchesFunc compares the parameter and result types of fn, one by one.
func (e *typeExpr) matchesFunc(fn *dst.FuncType, imports map[string]string) bool {
	return matchesTypeList(e.params, fn.Params, imports) && matchesTypeList(e.results, fn.Results, imports)
}

// matchesTypeList reports whether fields holds exactly the wanted types, in
// order. A field that declares several names, as in a, b int, counts once for
// each name.
func matchesTypeList(want []*typeExpr, fields *dst.FieldList, imports map[string]string) bool {
	var got []dst.Expr
	if fields != nil {
		for _, field := range fields.List {
			for range max(len(field.Names), 1) {
				got = append(got, field.Type)
			}
		}
	}
	if len(want) != len(got) {
		return false
	}
	for i, w := range want {
		if !w.matches(got[i], imports) {
			return false
		}
	}
	return true
}

// arrayLengthText returns the source text of an array length that is a number
// or a name, and "" for any other expression.
func arrayLengthText(length dst.Expr) string {
	switch l := length.(type) {
	case *dst.BasicLit:
		return l.Value
	case *dst.Ident:
		return l.Name
	}
	return ""
}
