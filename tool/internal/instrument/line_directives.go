// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
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
//   - A generated node can start with its own directive that Go reads (a
//     "/*line*/" comment, or a "//line" comment at the top level). This node
//     does not get a "<generated>" directive, and it keeps the position of its
//     own directive. The state becomes stale, so the next generated node gets
//     a "<generated>" directive.
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
// printed is in the <generated> file" (generated), "has a position that is
// not the user position, and is not in the <generated> file" (stale), or
// "has the user position" (known). It follows the code in source order. A block
// starts with the state that it has before its opening brace, and the state
// after a statement is the state after its last block. So a restore that was
// skipped (see below) is tried again at the next original node, also in the
// next block.
//
// A directive is written only when Go can read it. The pass does not write a
// directive in these cases, and the code keeps its wrong position (in the
// "<generated>" file, or in the user file when the state is stale) until a
// later node can be restored:
//
//   - The file name has a character that is not safe in a comment.
//   - The line is less than 1. Go does not accept it.
//   - The file name ends in ":" and digits. A directive has only a line, so Go
//     reads the end of the name as the line, and the line as the column.
//
// When the restore of a top-level declaration is skipped, and the declaration
// starts with a "//line" comment of the user, Go reads this comment. The code
// is then not in the "<generated>" file, so the state becomes stale.
//
// An import declaration with an added spec makes the state stale, because the
// printer sorts the specs. The next declaration is restored. The added spec
// does not get a "<generated>" directive.
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
//   - A user "//line" comment before the opening brace of a block (for
//     example between "else" and "{") is in the Start decorations of the
//     block. The state becomes stale, so the first statement of the block
//     gets a restore directive.
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
// The pass keeps the generated state only from a generated node to the next
// node of the same list, and only when the generated node has no line
// directive inside it or after it. After original code that it does not
// restore, the state is stale. The reason is that original code can hold a
// directive that Go reads in many places (for example, a "/*line*/" comment
// after a condition or after the body of a function literal), and the pass
// does not follow all of them.
//
// A stale state means that the pass does not know the position. The code can
// be in the user file at a wrong line, in the file of a directive that Go
// read (of the user, or of a generated node), or still in the "<generated>"
// file. So the next original node gets a restore directive, and the next
// generated node gets a "<generated>" directive. This is safe: these
// directives are correct, also when they are not necessary.
//
// A "/*line*/" comment of the user is not changed: it is valid at any column,
// and Go reads it. A "//line" comment of the user at the top level is at
// column 1, so it is also not changed. A "//line" directive that the pass
// writes at the top level is always at column 1: if a "/*...*/" comment is
// before it, the pass adds a newline.
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

// lineState is what the pass knows about the position that Go gives to the
// last token that was printed.
type lineState int

const (
	// lineKnown means that the position is the position in the user source.
	lineKnown lineState = iota
	// lineStale means that the pass does not know the position. It can be
	// in the user file at a wrong line, in another file, or in the
	// "<generated>" file. For example, a user "//line" comment is ignored, a
	// rule added code that the pass does not mark, or a restore was skipped.
	// The next original node needs a restore directive, and the next
	// generated node needs a "<generated>" directive.
	lineStale
	// lineGenerated means that the position is in the "<generated>" file. The
	// next original node needs a restore directive.
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
	// of the fields that are lists of decorations (see userDirectivesInside).
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
			// which spec is the last one, and the pass does not mark the
			// added spec. The lines after the declaration are off, so the
			// state becomes stale if it has an added spec. Then the next
			// declaration is restored. The state is not "generated",
			// because no "<generated>" directive was written. If the
			// restore is skipped, a generated node after it must still get
			// its directive.
			for _, spec := range gen.Specs {
				if _, ok = p.parser.OriginalPosition(spec); !ok {
					p.markStale(&state)
				} else {
					// The printer indents a user "//line" comment between
					// the specs, so Go ignores it.
					p.noteUserDirectives(p.userDirectivesInside(spec, true, true), &state)
				}
			}
			continue
		}
		// A block of the declaration can end in generated code, and the
		// restore of its closing brace can be skipped. Then the state is
		// stale, and the next declaration is restored.
		p.children(decl, &state)
	}
}

// step looks at one node of a list. state describes the last token that was
// printed. step adds the directive that the node needs, updates state, and
// reports whether the node is original. prev is the node before it in the same
// list, or nil.
func (p *lineDirectivePass) step(node, prev dst.Node, state *lineState) bool {
	pos, original := p.parser.OriginalPosition(node)
	if !original {
		// In a function body, the printer indents a "//line" comment, so
		// only a "/*line*/" comment can give the node its own position.
		p.stepGenerated(node, hasEffectiveDirective(node.Decorations().Start, false), state, func() {
			prependDirective(node, prev, "/*line "+p.generated+":1*/")
		})
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
// the directive was written. If it was not written, the position is wrong, and
// the state becomes stale. It does not stay generated: the node is original
// code, and it can hold a directive that Go reads (see the top of this file).
func (*lineDirectivePass) updateState(restored bool, state *lineState) {
	if restored {
		*state = lineKnown
	} else {
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
// reports whether it wrote a directive. When it did not, the node keeps the
// position that Go gives to it without the directive.
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
		// Do not write this directive. Without it, the node keeps a wrong
		// position (see updateState), which is safe.
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
// written (see the top of this file), the state becomes stale.
func (p *lineDirectivePass) stepDecl(decl dst.Decl, state *lineState) bool {
	pos, original := p.parser.OriginalPosition(decl)
	decs := decl.Decorations()
	ownDirective := hasEffectiveDirective(decs.Start, true)
	switch {
	case !original:
		p.stepGenerated(decl, ownDirective, state, func() {
			prependDeclDirective(decl, "//line "+p.generated+":1")
		})
	case *state != lineKnown:
		target, ok := directive(p.absolute(pos.Filename), pos.Line)
		if ok {
			prependDeclDirective(decl, "//line "+target)
			*state = lineKnown
		} else {
			*state = lineStale
		}
	}
	return original
}

// stepGenerated updates state for the generated node n. ownDirective is true
// when n starts with a directive that Go reads. Then n keeps the position of
// this directive, which is not in the "<generated>" file, and the state
// becomes stale. Otherwise mark writes the "<generated>" directive, unless the
// state is already generated, and the state becomes generated.
//
// A directive that Go reads can also be inside n or after it (for example, a
// "/*line*/" comment of the user in a statement that a rule wrapped). Then
// the code after this directive is not in the "<generated>" file, and the
// state becomes stale. The pass does not parse these comments: any decoration
// that has "//line " or "/*line " in it makes the state stale. A decoration
// can hold more than one comment, and the directive can be the second one.
func (p *lineDirectivePass) stepGenerated(n dst.Node, ownDirective bool, state *lineState, mark func()) {
	if ownDirective {
		*state = lineStale
		return
	}
	if *state != lineGenerated {
		mark()
	}
	*state = lineGenerated
	if p.mayHaveDirectiveInside(n) {
		*state = lineStale
	}
}

// mayHaveDirectiveInside reports whether n, or a node inside n, has a
// decoration that can hold a line directive (see mayHoldDirective). The Start
// decorations of n are not read, because they hold the directive that the pass
// writes.
func (p *lineDirectivePass) mayHaveDirectiveInside(n dst.Node) bool {
	found := false
	dst.Inspect(n, func(c dst.Node) bool {
		if c == nil || found {
			return false
		}
		lists := p.innerDecorations(c)
		lists = append(lists, c.Decorations().End)
		if c != n {
			lists = append(lists, c.Decorations().Start)
		}
		for _, list := range lists {
			found = found || mayHoldDirective(list)
		}
		return !found
	})
	return found
}

// mayHoldDirective reports whether a decoration in decorations has "//line "
// or "/*line " in it. This is true for every line directive, and also for some
// comments that are not directives.
func mayHoldDirective(decorations dst.Decorations) bool {
	for _, d := range decorations {
		if strings.Contains(d, directiveLineComment) || strings.Contains(d, directiveBlockComment) {
			return true
		}
	}
	return false
}

// prependDeclDirective adds a "//line" directive after the Start decorations
// of decl. Go reads a "//line" comment only at column 1. If the last
// decoration is a "/*...*/" comment, the printer puts the next decoration on
// the same line, so the function adds a newline before the directive.
func prependDeclDirective(decl dst.Decl, directive string) {
	decs := decl.Decorations()
	if decs.Before == dst.None {
		decs.Before = dst.NewLine
	}
	if n := len(decs.Start); n > 0 && decs.Start[n-1] != "\n" && !strings.HasPrefix(decs.Start[n-1], "//") {
		decs.Start.Append("\n")
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
// A user "/*line*/" comment inside n is read by Go, so a generated state
// becomes stale (see noteUserDirectives). The decorations of n itself (Start
// and End) are not checked: step and list do that.
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
	// effective is true when n has a user "/*line*/" comment. For the same
	// reason, a generated state after the walk becomes stale.
	found := false
	effective := false
	dst.Inspect(n, func(c dst.Node) bool {
		if c == nil {
			return false
		}
		if block, ok := c.(*dst.BlockStmt); ok {
			p.block(block, state)
			return false
		}
		if _, original := p.parser.OriginalPosition(c); !original {
			// The pass does not enter the node, so it does not know if
			// the node holds a directive that Go reads. The state becomes
			// stale, also if it was generated.
			found = true
			*state = lineStale
			return false
		}
		directives := p.userDirectivesInside(c, c != n && c != labeled, c != n)
		found = found || directives.ignored
		effective = effective || directives.effective
		p.noteUserDirectives(directives, state)
		if l, ok := c.(*dst.LabeledStmt); ok {
			labeled = l.Stmt
		}
		return true
	})
	p.noteUserDirectives(userDirectives{ignored: found, effective: effective}, state)
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
	p.noteUserDirectives(userDirectivesIn(expr.Decorations().Start, true), state)
	p.children(expr, state)
}

// userDirectives tells which user directives a part of the original code
// holds.
type userDirectives struct {
	// ignored is true for a "//line" comment that the printer indents, so
	// that Go ignores it.
	ignored bool
	// effective is true for a "/*line*/" comment, which Go reads.
	effective bool
}

// userDirectivesIn returns the user directives in decorations. startsLine is
// true when the first decoration is at the start of a line (see
// hasUserLineDirective). The decorations must not be at the top level.
func userDirectivesIn(decorations dst.Decorations, startsLine bool) userDirectives {
	return userDirectives{
		ignored:   hasUserLineDirective(decorations, startsLine),
		effective: hasEffectiveDirective(decorations, false),
	}
}

// noteUserDirectives updates state for the user directives of a part of the
// original code. Go ignores a "//line" comment, so a known state becomes
// stale. Go reads a "/*line*/" comment, so the code after it is not in the
// "<generated>" file, and a generated state becomes stale. A known state stays
// known, because the positions of the pass follow the directive.
func (*lineDirectivePass) noteUserDirectives(directives userDirectives, state *lineState) {
	switch {
	case directives.ignored && *state == lineKnown:
		*state = lineStale
	case directives.effective && *state == lineGenerated:
		*state = lineStale
	}
}

// userDirectivesInside returns the user directives in the decorations of n.
// The nodes keep some comments in fields of their own, for example the comment
// after "(" of a call, so the function reads all the fields of the type
// dst.Decorations. It reads Start only when start is true, and End only when
// end is true.
func (p *lineDirectivePass) userDirectivesInside(n dst.Node, start, end bool) userDirectives {
	var result userDirectives
	add := func(found userDirectives) {
		result.ignored = result.ignored || found.ignored
		result.effective = result.effective || found.effective
	}
	decs := n.Decorations()
	if start {
		add(userDirectivesIn(decs.Start, true))
	}
	if end {
		add(userDirectivesIn(decs.End, false))
	}
	for _, list := range p.innerDecorations(n) {
		add(userDirectivesIn(list, false))
	}
	return result
}

// innerDecorations returns the lists of decorations that n keeps in fields of
// its own (all the fields of the type dst.Decorations), other than Start and
// End.
func (p *lineDirectivePass) innerDecorations(n dst.Node) []dst.Decorations {
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
	lists := make([]dst.Decorations, 0, len(fields))
	for _, i := range fields {
		if list, isList := reflect.TypeAssert[dst.Decorations](inner.Field(i)); isList {
			lists = append(lists, list)
		}
	}
	return lists
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
		// A rule added the block, so its number of lines is not known. The
		// pass does not enter it, so the state becomes stale, also if it was
		// generated.
		*state = lineStale
		return
	}
	// A user "//line" comment before the opening brace is indented by the
	// printer, so Go ignores it. The first statement needs a restore.
	p.noteUserDirectives(userDirectivesIn(block.Decs.Start, true), state)
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
		// The closing brace is original code, and the decorations of the
		// block can hold a directive that Go reads. So the state does not
		// stay generated.
		*state = lineStale
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
	} else {
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
		p.noteUserDirectives(userDirectivesIn(stmt.Decorations().End, false), state)
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

// hasEffectiveDirective reports whether decorations hold a line directive that
// Go reads. A "/*line*/" comment is valid at any column. A "//line" comment is
// valid only at column 1. The printer puts it there only at the top level, so
// it counts only when topLevel is true, and when it starts a line.
func hasEffectiveDirective(decorations dst.Decorations, topLevel bool) bool {
	atLineStart := true
	for _, d := range decorations {
		block := strings.HasPrefix(d, directiveBlockComment)
		if (block || topLevel && atLineStart) && isLineDirective(d) {
			return true
		}
		atLineStart = d == "\n" || strings.HasPrefix(d, "//")
	}
	return false
}

// isLineDirective reports whether comment is one comment that has the form of
// a line directive: "//line" or "/*line", a space, and text that ends in ":"
// and a number from 1 to maxDirectiveLine. A comment such as "//line of code"
// is not a directive. A decoration that holds more than one comment (for
// example "/*line a*/ /* b:1*/") is not a directive, because Go reads only the
// first comment.
func isLineDirective(comment string) bool {
	var text string
	switch {
	case strings.HasPrefix(comment, directiveLineComment):
		text = comment[len(directiveLineComment):]
		if strings.ContainsAny(text, "\r\n") {
			return false
		}
	case strings.HasPrefix(comment, directiveBlockComment) && strings.HasSuffix(comment, "*/"):
		text = comment[len(directiveBlockComment) : len(comment)-len("*/")]
		if strings.Contains(text, "*/") {
			return false
		}
	default:
		return false
	}
	i := strings.LastIndexByte(text, ':')
	if i < 0 {
		return false
	}
	line, err := strconv.ParseUint(text[i+1:], 10, 0)
	return err == nil && line > 0 && line <= maxDirectiveLine
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
