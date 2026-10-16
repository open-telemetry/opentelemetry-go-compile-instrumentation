// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/internal/ast"
)

// -----------------------------------------------------------------------------
// Line Directives
//
// Instrumentation puts code in a file that the user did not write. Without
// help, a stack trace or a debugger shows this code as lines of the user file,
// and every line after it is off. This pass fixes both problems with line
// directives.
//
// A "//line" comment is only valid at column 1, but the printer indents
// comments in a function body. A "/*line*/" comment is valid at any column, and
// it applies to the character that follows it.
//
// A directive carries only a line. It has no column, so Go does not know the
// column of the code after a directive. The reason is that the printer does not
// always put a comment where the pass expects (for example, a comment on an
// argument that is not the first goes before the ","). A column from the
// source would then be wrong.
//
// The pass finds runs of generated code. A run is a sequence of nodes in the
// same list that are not in the user source:
//
//   - The first node of a run gets "/*line /<generated>:1*/". All the nodes of
//     the run get a line number in the "/<generated>" file. The name is
//     absolute and OS-specific (see generatedFile), so it is the same in all
//     Go versions.
//   - The first original node after a run gets "/*line /abs/file.go:L*/",
//     where L is the line of the node in the user source.
//   - A run can be the last thing in a block. Then the closing brace must be
//     restored. The last node of the block gets an End decoration with a
//     directive for the line before the closing brace. The printer puts a
//     newline after this directive, so the brace starts on the original line.
//
// The printer does not always put the comment and the token on the same line.
// The pass handles the cases that it knows:
//
//   - A blank line before the node. The pass removes the blank line.
//   - A comment before the node, and the clause of a switch or select. The
//     directive is on a line of its own, followed by a newline. It gives the
//     line before the node.
//   - A top-level declaration. The printer puts a blank line between most of
//     them. The directive is a "//line" comment at column 1. It gives the line
//     of the declaration and applies to the next line.
//
// The clauses of a switch or select have no closing brace. The pass handles the
// clauses of one statement as one list, so a run that ends a clause is restored
// at the next clause.
//
// The pass keeps one state for the whole file: "the last token that was
// printed is generated code" (or, stale, "is code with a wrong position"). It follows the code in source order. A block
// starts with the state that it has before its opening brace, and the state
// after a statement is the state after its last block. So a restore that was
// skipped (see below) is tried again at the next original node, also in the
// next block.
//
// A directive is written only when Go can read it. The pass does not write a
// directive in these cases, and the code stays in the "<generated>" file until
// a later node can be restored:
//
//   - The file name has a character that is not safe in a comment.
//   - The line is less than 1. Go does not accept it.
//   - The file name ends in ":" and digits. A directive has only a line, so Go
//     reads the end of the name as the line, and the line as the column.
//
// An import declaration with an added spec counts as generated, because the
// printer sorts the specs. The next declaration is restored.
//
// A "//line" comment of the user can be in a function body (for example, the
// code that goyacc writes). The printer indents it, so Go does not read it as
// a directive, and the lines after it are off. The pass keeps the comment and
// restores the position:
//
//   - An original node that has a user "//line" comment in its Start
//     decorations gets a restore directive. The comment is on a line of its
//     own, so the directive uses the own-line form (see above). The position of
//     the node already has the effect of the user directive.
//   - A user "//line" comment in the End decorations of a node makes the state
//     "stale". The next original node, or the closing brace of the block, gets
//     a restore directive.
//   - A user "//line" comment in an empty block gets a restore directive after
//     it, before the closing brace of the block, in the same way.
//   - A user "//line" comment between a label and its statement is in the
//     decorations of the statement. The statement gets a restore directive.
//
// Code in an original statement can also move the lines. This is the case for
// a node that is not original (for example, the wrapper that a call rule puts
// around a call, or a field that a struct rule adds to a type declaration),
// and for a user "//line" comment inside an expression (for example, in the
// arguments of a call). The pass finds this code in the statement, but not in
// a block of the statement. A known state becomes "stale" when the pass finds
// it, and again after the statement, because some comments of a node are
// printed after its blocks. So the next original node, the closing brace of
// the block, or the next top-level declaration is restored. A user "//line"
// comment between the specs of a grouped import makes the next declaration
// restored in the same way. The generated code itself is not marked:
// it keeps the lines of the statement. This is a known limit.
//
// A stale state is like a generated state, but the code is still in the user
// file. So the pass does not write a "<generated>" directive for it, unless a
// generated node follows. A "/*line*/" comment of the user is not changed: it
// is valid at any column. A "//line" comment of the user at the top level is
// at column 1, so it is also not changed.
//
// The pass does not enter a generated node. The pass uses the adjusted
// position of a node, so a "//line" directive that is already in the source (for
// example in a cgo file) keeps its effect.
// -----------------------------------------------------------------------------

// generatedName is the base name of the file that holds the generated code.
const generatedName = "<generated>"

const (
	// directiveLineComment and directiveBlockComment are the starts of a line
	// directive in a comment.
	directiveLineComment  = "//line "
	directiveBlockComment = "/*line "
)

// lineState is what the pass knows about the line of the last token that was
// printed.
type lineState int

const (
	// lineKnown means that Go gives the right position to the next token.
	lineKnown lineState = iota
	// lineStale means that the last token is user code, but Go does not give
	// it the right position, because a user "//line" comment is ignored. The
	// next original node needs a restore directive.
	lineStale
	// lineGenerated means that the last token is generated code. The next
	// original node needs a restore directive.
	lineGenerated
)

// lineDirectivePass adds the line directives to one file.
type lineDirectivePass struct {
	parser *ast.AstParser
	// dir is the directory of the original file. It is the base for a relative
	// file name in a position.
	dir string
	// generated is the file name of the generated code in a directive.
	generated string
	// decorationFields holds, for each type of node decorations, the indexes
	// of the fields that are lists of decorations (see hasUserDirectiveInside).
	decorationFields map[reflect.Type][]int
}

// generatedFile returns the file name that line directives give to generated
// code in the file absFile. The name is absolute, because Go 1.27 joins a
// relative name in a directive with the directory of the file, and Go 1.26
// does not. With an absolute name, the position is the same in all Go
// versions. The name has the volume of absFile, because on Windows a name
// without a volume is not absolute. On Unix the volume is empty, so the name
// is "/<generated>". If the name is not safe in a comment, the function
// returns "<generated>".
func generatedFile(absFile string) string {
	return generatedFileOnVolume(filepath.VolumeName(absFile))
}

// generatedFileOnVolume returns the generated file name at the root of
// volume. It is separate from generatedFile, so that a test can give a volume
// that is not safe on every OS.
func generatedFileOnVolume(volume string) string {
	name := volume + string(filepath.Separator) + generatedName
	if !safeDirectiveFile(name) {
		return generatedName
	}
	return name
}

// markLineDirectives adds the line directives to root. parser must be the
// parser that read the original file at path file, and file must be absolute.
func markLineDirectives(parser *ast.AstParser, root *dst.File, file string) {
	p := &lineDirectivePass{
		parser:           parser,
		dir:              filepath.Dir(file),
		generated:        generatedFile(file),
		decorationFields: map[reflect.Type][]int{},
	}
	state := lineKnown
	for _, decl := range root.Decls {
		original := p.stepDecl(decl, &state)
		if !original {
			continue
		}
		if gen, ok := decl.(*dst.GenDecl); ok && gen.Tok == token.IMPORT {
			// An added import is a spec in an original import declaration.
			// The printer sorts the specs, so it is not possible to know
			// which spec is the last one. Mark all the declaration as
			// generated if it has an added spec. Then the next declaration
			// is restored.
			for _, spec := range gen.Specs {
				if _, ok = p.parser.OriginalPosition(spec); !ok {
					state = lineGenerated
				} else if p.hasUserDirectiveInside(spec, true, true) {
					// The printer indents a user "//line" comment between
					// the specs, so Go ignores it.
					p.markStale(&state)
				}
			}
			continue
		}
		// A block of the declaration can end in generated code, and its
		// closing brace is not restored. Then generated stays true.
		p.children(decl, &state)
	}
}

// step looks at one node of a list. state describes the last token that was
// printed. step adds the directive that the node
// needs, updates generated, and reports whether the node is original. prev is
// the node before it in the same list, or nil.
func (p *lineDirectivePass) step(node, prev dst.Node, state *lineState) bool {
	pos, original := p.parser.OriginalPosition(node)
	if !original {
		if *state != lineGenerated && !hasLineDirective(node.Decorations().Start) {
			prependDirective(node, prev, "/*line "+p.generated+":1*/")
		}
		*state = lineGenerated
		return false
	}
	// A user "//line" comment before the node is indented by the printer, so
	// Go ignores it. The node then needs a restore, also if the state is known.
	userDirective := hasUserLineDirective(node.Decorations().Start, true)
	restored := false
	if *state != lineKnown || userDirective {
		restored = p.restore(node, prev, pos)
		p.updateState(restored, state)
	}
	if labeled, isLabeled := node.(*dst.LabeledStmt); isLabeled {
		p.stepLabeled(labeled, restored, state)
	}
	return true
}

// updateState sets the state after a restore of a node. restored is true when
// the directive was written.
func (*lineDirectivePass) updateState(restored bool, state *lineState) {
	if restored {
		*state = lineKnown
	} else if *state == lineKnown {
		// The directive was not written, so the position is wrong.
		*state = lineStale
	}
}

// stepLabeled looks at the statement of a label. The printer puts the label on
// one line and the statement on the next line. So when the label got a
// directive (labelRestored is true), the statement needs its own directive. It
// needs one also when it has a user "//line" comment before it, because the
// comment is in the decorations of the statement, and not in those of the
// label.
func (p *lineDirectivePass) stepLabeled(labeled *dst.LabeledStmt, labelRestored bool, state *lineState) {
	inner := labeled.Stmt
	pos, original := p.parser.OriginalPosition(inner)
	if !original {
		// A rule replaced the statement of the label with generated code.
		// Mark it like a generated statement in a list.
		p.step(inner, nil, state)
		return
	}
	restored := false
	if labelRestored || *state != lineKnown || hasUserLineDirective(inner.Decorations().Start, true) {
		restored = p.restore(inner, nil, pos)
		p.updateState(restored, state)
	}
	if next, isLabeled := inner.(*dst.LabeledStmt); isLabeled {
		// A label on a label.
		p.stepLabeled(next, restored, state)
	}
}

// restore adds the directive that maps node to its original position pos. It
// reports whether it wrote a directive. When it did not, the node stays in the
// "<generated>" file.
//
// Known limitation: a directive gives the position of one token, and the
// printer then counts lines by itself. If the user source has two statements on
// the same line ("a(); b()"), the printer puts them on two lines, and the
// lines after them are off by one until the next directive. gofmt does not
// produce such code.
func (p *lineDirectivePass) restore(node, prev dst.Node, pos token.Position) bool {
	file := p.absolute(pos.Filename)
	_, isClause := node.(*dst.CaseClause)
	_, isComm := node.(*dst.CommClause)
	ownLine := len(node.Decorations().Start) > 0 || isClause || isComm
	var target string
	var ok bool
	switch {
	case ownLine:
		// The printer puts the directive on a line of its own when the node
		// has a comment before it, and for the clause of a switch. A newline
		// follows the directive, so it gives the line before the node.
		target, ok = directive(file, pos.Line-1)
	default:
		target, ok = directive(file, pos.Line)
	}
	if !ok {
		// Do not write this directive. Without it, the node stays in the
		// "<generated>" file, which is safe.
		return false
	}
	if ownLine {
		decs := node.Decorations()
		decs.Before = dst.NewLine
		decs.Start.Append("/*line "+target+"*/", "\n")
		return true
	}
	prependDirective(node, prev, "/*line "+target+"*/")
	return true
}

// stepDecl is like step, but for a top-level declaration. The printer puts a
// blank line between most declarations, and then a /*...*/ comment would be on
// a line of its own. So the directive is a "//line" comment, which is valid
// at column 1, and applies to the next line. When the restore directive is not
// written (see the top of this file), state does not change.
func (p *lineDirectivePass) stepDecl(decl dst.Decl, state *lineState) bool {
	pos, original := p.parser.OriginalPosition(decl)
	decs := decl.Decorations()
	switch {
	case !original:
		if *state != lineGenerated && !hasLineDirective(decs.Start) {
			prependDeclDirective(decl, "//line "+p.generated+":1")
		}
		*state = lineGenerated
	case *state != lineKnown:
		target, ok := directive(p.absolute(pos.Filename), pos.Line)
		if ok {
			prependDeclDirective(decl, "//line "+target)
			*state = lineKnown
		}
	}
	return original
}

func prependDeclDirective(decl dst.Decl, directive string) {
	decs := decl.Decorations()
	if decs.Before == dst.None {
		decs.Before = dst.NewLine
	}
	decs.Start.Append(directive)
}

// children processes the code that is inside the original node n, in source
// order. It processes the blocks, and it looks for code that moves the lines
// of the printed file:
//
//   - A node that is not original (for example an expression or a block that
//     a rule added). Its number of lines is not known, so the code after it
//     has a wrong position. The pass does not enter this node.
//   - A user "//line" comment inside an expression, for example in the
//     arguments of a call. The printer indents it, so Go ignores it.
//
// For both, a known state becomes stale, and the next original node (or the
// closing brace of the block) is restored. The code that was added is not
// marked. This is a known limit: it stays in the position of the user file.
// The decorations of n itself (Start and End) are not checked: step and list
// do that.
//
// state is the state before n. When children returns, state is the state
// after the last part of n. It is not lineKnown when that part ended in code
// with a wrong position, and the directive for its closing brace was not
// written. Then the caller must restore the code that follows n.
func (p *lineDirectivePass) children(n dst.Node, state *lineState) {
	// The Start decorations of the statement of a label are handled by
	// stepLabeled.
	var labeled dst.Node
	// found is true when n has generated code or a user "//line" comment. Some
	// decorations of a node are printed after its child blocks (for example the
	// comment before ")" of a call), but Inspect sees them before. So the state
	// is also made stale after the walk: the code after n is then restored.
	found := false
	dst.Inspect(n, func(c dst.Node) bool {
		if c == nil {
			return false
		}
		if block, ok := c.(*dst.BlockStmt); ok {
			p.block(block, state)
			return false
		}
		if _, original := p.parser.OriginalPosition(c); !original {
			found = true
			p.markStale(state)
			return false
		}
		if p.hasUserDirectiveInside(c, c != n && c != labeled, c != n) {
			found = true
			p.markStale(state)
		}
		if l, ok := c.(*dst.LabeledStmt); ok {
			labeled = l.Stmt
		}
		return true
	})
	if found {
		p.markStale(state)
	}
}

// markStale changes a known state to stale. A generated state does not change.
func (*lineDirectivePass) markStale(state *lineState) {
	if *state == lineKnown {
		*state = lineStale
	}
}

// operand is like children, for an expression or a statement in the header of
// a clause. No other function looks at its Start. Its End cannot have a user
// "//line" comment, because a newline before the ":" is not valid.
func (p *lineDirectivePass) operand(expr dst.Node, state *lineState) {
	if hasUserLineDirective(expr.Decorations().Start, true) {
		p.markStale(state)
	}
	p.children(expr, state)
}

// hasUserDirectiveInside reports whether the decorations of n hold a user
// "//line" comment. The nodes keep some comments in fields of their own, for
// example the comment after "(" of a call, so the function reads all the
// fields of the type dst.Decorations. It reads Start only when start is true,
// and End only when end is true.
func (p *lineDirectivePass) hasUserDirectiveInside(n dst.Node, start, end bool) bool {
	decs := n.Decorations()
	if start && hasUserLineDirective(decs.Start, true) || end && hasUserLineDirective(decs.End, false) {
		return true
	}
	inner := reflect.ValueOf(n).Elem().FieldByName("Decs")
	typ := inner.Type()
	fields, ok := p.decorationFields[typ]
	if !ok {
		for i := range typ.NumField() {
			if typ.Field(i).Type == reflect.TypeFor[dst.Decorations]() {
				fields = append(fields, i)
			}
		}
		p.decorationFields[typ] = fields
	}
	for _, i := range fields {
		list, isList := reflect.TypeAssert[dst.Decorations](inner.Field(i))
		if isList && hasUserLineDirective(list, false) {
			return true
		}
	}
	return false
}

// block processes the statements of a block. If the block is not original,
// it does not enter it, and a known state becomes stale. state is the state
// before the opening brace of the block. When block returns, state is the
// state after the closing brace: it is not lineKnown when the last token of
// the block has a wrong position, and the directive for the closing brace was
// not written.
func (p *lineDirectivePass) block(block *dst.BlockStmt, state *lineState) {
	rbrace, original := p.parser.Rbrace(block)
	if !original {
		// A rule added the block, so its number of lines is not known.
		p.markStale(state)
		return
	}
	if len(block.List) == 0 {
		p.emptyBlock(block, rbrace, state)
		return
	}
	p.list(block.List, state)
	if *state == lineKnown {
		return
	}
	// The closing brace is original, but the last token is not. Put the
	// directive before the newline that precedes the brace.
	target, ok := directive(p.absolute(rbrace.Filename), rbrace.Line-1)
	if !ok {
		return
	}
	decs := endNode(block.List).Decorations()
	if decs.After == dst.EmptyLine {
		decs.After = dst.NewLine
	}
	decs.End.Append("/*line " + target + "*/")
	*state = lineKnown
}

// emptyBlock processes a block without statements. The closing brace has a
// wrong position in two cases:
//
//   - The state before the block is not known. For example, a rule put code
//     of several lines in the condition of an "if" statement.
//   - A user "//line" comment is in the block. Go ignores it.
//
// Then the pass puts a restore directive after the opening brace (and after the
// comments), before the closing brace. If it cannot write the directive, the
// code after the block needs a restore.
func (p *lineDirectivePass) emptyBlock(block *dst.BlockStmt, rbrace token.Position, state *lineState) {
	userDirective := hasUserLineDirective(block.Decs.Lbrace, false)
	if *state == lineKnown && !userDirective {
		return
	}
	// The pass puts a newline after the directive, so the directive gives the
	// line before the brace.
	if target, ok := directive(p.absolute(rbrace.Filename), rbrace.Line-1); ok {
		block.Decs.Lbrace.Append("/*line "+target+"*/", "\n")
		*state = lineKnown
	} else if *state == lineKnown {
		*state = lineStale
	}
}

// list processes the statements of a block, or the clauses of a switch or
// select statement. The clauses share state, because a clause has no closing
// brace.
func (p *lineDirectivePass) list(stmts []dst.Stmt, state *lineState) {
	var prev dst.Node
	for _, stmt := range stmts {
		original := p.step(stmt, prev, state)
		prev = stmt
		if !original {
			continue
		}
		switch clause := stmt.(type) {
		case *dst.CaseClause:
			for _, expr := range clause.List {
				p.operand(expr, state)
			}
			p.list(clause.Body, state)
		case *dst.CommClause:
			if clause.Comm != nil {
				p.operand(clause.Comm, state)
			}
			p.list(clause.Body, state)
		default:
			p.children(stmt, state)
		}
		// A user "//line" comment after the node is ignored by Go. The next
		// original node needs a restore.
		if *state == lineKnown && hasUserLineDirective(stmt.Decorations().End, false) {
			*state = lineStale
		}
	}
}

// endNode returns the node that gets the End decoration with the directive
// for the closing brace of a block. It is the node that the code of the last
// statement ends with. If a node on the way has End decorations, it is this
// node. The printer prints the End decorations of a clause after its body, so
// a comment there is the last thing before the brace. The directive must follow
// the comment, else the newlines of the comment move the brace.
func endNode(stmts []dst.Stmt) dst.Node {
	last := stmts[len(stmts)-1]
	if len(last.Decorations().End) > 0 {
		return last
	}
	switch clause := last.(type) {
	case *dst.CaseClause:
		if len(clause.Body) > 0 {
			return endNode(clause.Body)
		}
	case *dst.CommClause:
		if len(clause.Body) > 0 {
			return endNode(clause.Body)
		}
	}
	return last
}

// safeDirectiveFile reports whether file can be written in a line directive.
// A name that comes from a "//line" comment in the user source (for example a
// cgo file) can have any characters. "*/" ends a block comment, and a line
// break ends a line comment. Code after them would be a part of the program.
// The same rule is used for both kinds of comment. The Go scanner also rejects
// a byte order mark (U+FEFF), a NUL character, and invalid UTF-8 in a comment,
// and then the printer fails.
func safeDirectiveFile(file string) bool {
	return utf8.ValidString(file) &&
		!strings.ContainsAny(file, "\r\n\x00\uFEFF") &&
		!strings.Contains(file, "*/")
}

// directive returns the target of a line directive, "file:line", and reports
// whether Go can read it. It is not possible to write a directive when:
//
//   - the file name is not safe in a comment (see safeDirectiveFile),
//   - the line is less than 1 or too large,
//   - the file name ends in ":" and digits. Go reads the last two numbers as the
//     line and the column, so it takes the end of the name as the line.
func directive(file string, line int) (string, bool) {
	if !safeDirectiveFile(file) || line < 1 || line > maxDirectiveLine || endsWithNumber(file) {
		return "", false
	}
	return fmt.Sprintf("%s:%d", file, line), true
}

// maxDirectiveLine is the largest line that Go accepts in a line
// directive (syntax.PosMax in cmd/compile, maxLineCol in go/scanner). A user
// directive near this limit can give later nodes a larger position.
const maxDirectiveLine = 1 << 30

// endsWithNumber reports whether file ends in ":" and digits.
func endsWithNumber(file string) bool {
	i := len(file)
	for i > 0 && file[i-1] >= '0' && file[i-1] <= '9' {
		i--
	}
	return i < len(file) && i > 0 && file[i-1] == ':'
}

// absolute returns the absolute name of file. An empty name stays empty: it
// comes from a "//line :L" comment of the user, and Go gives the empty name to
// the code after it.
func (p *lineDirectivePass) absolute(file string) string {
	if file == "" || filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(p.dir, file)
}

// prependDirective puts directive immediately before the first token of node.
// prev is the node before it in the same list, or nil.
//
// The printer puts a /*...*/ comment on a line of its own when a blank line is
// before it. Then the comment applies to the newline and not to the token. To
// prevent this, the function removes the blank line before the node.
func prependDirective(node, prev dst.Node, directive string) {
	decs := node.Decorations()
	decs.Before = dst.NewLine
	if prev != nil {
		if prevDecs := prev.Decorations(); prevDecs.After == dst.EmptyLine {
			prevDecs.After = dst.NewLine
		}
	}
	decs.Start.Append(directive)
}

// hasLineDirective reports whether decorations hold a line directive.
func hasLineDirective(decorations dst.Decorations) bool {
	for _, d := range decorations {
		if strings.HasPrefix(d, directiveLineComment) || strings.HasPrefix(d, directiveBlockComment) {
			return true
		}
	}
	return false
}

// hasUserLineDirective reports whether decorations hold a "//line" comment of
// the user that Go would read as a directive if the printer did not indent it.
// Such a comment is at the start of a line: it is the first decoration of a
// Start list (startsLine is true), or it follows a newline or a line comment.
// A comment after code on the same line is not a directive. A directive that
// the pass wrote is a "/*line*/" comment, so it does not match.
func hasUserLineDirective(decorations dst.Decorations, startsLine bool) bool {
	atLineStart := startsLine
	for _, d := range decorations {
		if atLineStart && strings.HasPrefix(d, directiveLineComment) {
			return true
		}
		atLineStart = d == "\n" || strings.HasPrefix(d, "//")
	}
	return false
}
