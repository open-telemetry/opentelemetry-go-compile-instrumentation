// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"go/token"
	"path/filepath"
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
//   - The first node of a run gets "/*line <generated>:1*/". All the nodes of
//     the run get a line number in the "<generated>" file.
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
// printed is generated code". It follows the code in source order. A block
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
// The pass does not enter a generated node. The pass uses the adjusted
// position of a node, so a "//line" directive that is already in the source (for
// example in a cgo file) keeps its effect.
// -----------------------------------------------------------------------------

const (
	// generatedDirective marks the first node of a run of generated code.
	generatedDirective = "/*line <generated>:1*/"
	// directiveLineComment and directiveBlockComment are the starts of a line
	// directive in a comment.
	directiveLineComment  = "//line "
	directiveBlockComment = "/*line "
)

// lineDirectivePass adds the line directives to one file.
type lineDirectivePass struct {
	parser *ast.AstParser
	// dir is the directory of the original file. It is the base for a relative
	// file name in a position.
	dir string
}

// markLineDirectives adds the line directives to root. parser must be the
// parser that read the original file at path file, and file must be absolute.
func markLineDirectives(parser *ast.AstParser, root *dst.File, file string) {
	p := &lineDirectivePass{parser: parser, dir: filepath.Dir(file)}
	generated := false
	for _, decl := range root.Decls {
		original := p.stepDecl(decl, &generated)
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
					generated = true
				}
			}
			continue
		}
		// A block of the declaration can end in generated code, and its
		// closing brace is not restored. Then generated stays true.
		p.children(decl, &generated)
	}
}

// step looks at one node of a list. generated is true when the last token
// that was printed is generated code. step adds the directive that the node
// needs, updates generated, and reports whether the node is original. prev is
// the node before it in the same list, or nil.
func (p *lineDirectivePass) step(node, prev dst.Node, generated *bool) bool {
	pos, original := p.parser.OriginalPosition(node)
	if !original {
		if !*generated && !hasLineDirective(node.Decorations().Start) {
			prependDirective(node, prev, generatedDirective)
		}
		*generated = true
		return false
	}
	if *generated && p.restore(node, prev, pos) {
		*generated = false
	}
	return true
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
	if labeled, isLabeled := node.(*dst.LabeledStmt); isLabeled {
		// The printer puts the label on one line and the statement on the
		// next line. The statement needs its own directive.
		defer func() {
			if innerPos, original := p.parser.OriginalPosition(labeled.Stmt); original {
				p.restore(labeled.Stmt, nil, innerPos)
			}
		}()
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
// written (see the top of this file), generated stays true.
func (p *lineDirectivePass) stepDecl(decl dst.Decl, generated *bool) bool {
	pos, original := p.parser.OriginalPosition(decl)
	decs := decl.Decorations()
	switch {
	case !original:
		if !*generated && !hasLineDirective(decs.Start) {
			prependDeclDirective(decl, "//line <generated>:1")
		}
		*generated = true
	case *generated:
		target, ok := directive(p.absolute(pos.Filename), pos.Line)
		if ok {
			prependDeclDirective(decl, "//line "+target)
			*generated = false
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

// children processes the blocks that are inside the original node n, in source
// order. It does not enter a node that is not original, as all its code is
// generated. generated is the state before n. When children returns, generated
// is the state after the last block of n: it is true when that block ended in
// generated code, and the directive for its closing brace was not written.
// Then the caller must treat the code that follows n as generated.
func (p *lineDirectivePass) children(n dst.Node, generated *bool) {
	dst.Inspect(n, func(c dst.Node) bool {
		block, ok := c.(*dst.BlockStmt)
		if !ok {
			return true
		}
		p.block(block, generated)
		return false
	})
}

// block processes the statements of an original block. generated is the state
// before the opening brace of the block. When block returns, generated is the
// state after the closing brace: it is true when the last token of the block
// is generated code, and the directive for the closing brace was not written.
func (p *lineDirectivePass) block(block *dst.BlockStmt, generated *bool) {
	rbrace, original := p.parser.Rbrace(block)
	if !original {
		return
	}
	p.list(block.List, generated)
	if !*generated || len(block.List) == 0 {
		return
	}
	// The closing brace is original, but the last token is not. Put the
	// directive before the newline that precedes the brace.
	target, ok := directive(p.absolute(rbrace.Filename), rbrace.Line-1)
	if !ok {
		return
	}
	decs := lastNode(block.List).Decorations()
	if decs.After == dst.EmptyLine {
		decs.After = dst.NewLine
	}
	decs.End.Append("/*line " + target + "*/")
	*generated = false
}

// list processes the statements of a block, or the clauses of a switch or
// select statement. The clauses share generated, because a clause has no
// closing brace.
func (p *lineDirectivePass) list(stmts []dst.Stmt, generated *bool) {
	var prev dst.Node
	for _, stmt := range stmts {
		original := p.step(stmt, prev, generated)
		prev = stmt
		if !original {
			continue
		}
		switch clause := stmt.(type) {
		case *dst.CaseClause:
			for _, expr := range clause.List {
				p.children(expr, generated)
			}
			p.list(clause.Body, generated)
		case *dst.CommClause:
			if clause.Comm != nil {
				p.children(clause.Comm, generated)
			}
			p.list(clause.Body, generated)
		default:
			p.children(stmt, generated)
		}
	}
}

// lastNode returns the node that the code of the last statement ends with.
func lastNode(stmts []dst.Stmt) dst.Node {
	last := stmts[len(stmts)-1]
	switch clause := last.(type) {
	case *dst.CaseClause:
		if len(clause.Body) > 0 {
			return lastNode(clause.Body)
		}
	case *dst.CommClause:
		if len(clause.Body) > 0 {
			return lastNode(clause.Body)
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

func (p *lineDirectivePass) absolute(file string) string {
	if filepath.IsAbs(file) {
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
