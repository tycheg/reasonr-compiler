// Package parser builds the ReasonR syntax tree by recursive descent.
//
// The parser does not stop at the first error: unparsed places become Bad*
// nodes, and parsing continues from the next line. At most one syntax
// error is reported per line.
package parser

import (
	"unicode"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/lexer"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// MaxErrors is how many syntax errors are reported per file.
const MaxErrors = 100

// ParseFile adds a file to the set and parses it.
func ParseFile(fset *source.FileSet, name string, src []byte) (*ast.File, []*diag.Diagnostic) {
	return Parse(fset.AddFile(name, src))
}

// Parse parses a file already added to the set. It returns the tree (never
// nil) and the lexer and parser diagnostics.
func Parse(f *source.File) (*ast.File, []*diag.Diagnostic) {
	toks, comments, lexDiags := lexer.Scan(f)
	errs := &errorState{file: f, lines: map[int]bool{}}
	for _, d := range lexDiags {
		errs.lines[f.Line(d.Range.Start)] = true
	}
	errs.bag.Append(lexDiags...)

	p := &parser{file: f, toks: toks, errs: errs}
	p.collectDocs(comments)
	p.tok = toks[0]
	p.prevEnd = f.Pos(0)
	file := p.parseFile()
	file.Comments = comments
	return file, errs.bag.List()
}

// errorState is the error state shared by the parser and nested interpolation parsers.
type errorState struct {
	file    *source.File
	bag     diag.Bag
	lines   map[int]bool // lines that already have an error
	count   int
	stopped bool
	lastPos source.Pos // position of the last error, suppressed ones included
}

type parser struct {
	file *source.File
	toks []token.Token
	i    int
	tok  token.Token
	errs *errorState

	// prevEnd is the end of the last significant token read (not NEWLINE,
	// INDENT, DEDENT).
	prevEnd source.Pos
	// lineDone: the logical line is already finished by a nested block
	// (search sections), no NEWLINE is expected.
	lineDone bool

	// layout: a layout is being parsed, an argument may be a bare new.
	layout bool
	// test > 0: a test section is being parsed; a call in a statement block
	// is a test step, time offset is a clock shift.
	test int
	// uiTypes: primitive types are being parsed; the constraints id, enum,
	// value and screen are types too.
	uiTypes bool

	leadDocs map[int]*ast.CommentGroup // declaration line → "##" above it
	lineDocs map[int]*ast.CommentGroup // line → "##" at the end of that line
}

// ---------------------------------------------------------------------------
// Tokens

func (p *parser) next() {
	switch p.tok.Kind {
	case token.NEWLINE, token.INDENT, token.DEDENT, token.EOF:
	default:
		p.prevEnd = p.tok.End
	}
	if p.i < len(p.toks)-1 {
		p.i++
	}
	p.tok = p.toks[p.i]
}

func (p *parser) peek(n int) token.Token {
	if p.i+n < len(p.toks) {
		return p.toks[p.i+n]
	}
	return p.toks[len(p.toks)-1]
}

func (p *parser) at(k token.Kind) bool { return p.tok.Kind == k }

// atWord reports whether a contextual word is at the current position.
func (p *parser) atWord(word string) bool {
	return p.tok.Kind == token.IDENT && p.tok.Text == word
}

func isWord(t token.Token, word string) bool {
	return t.Kind == token.IDENT && t.Text == word
}

// atLineEnd reports whether the logical line has ended.
func (p *parser) atLineEnd() bool {
	switch p.tok.Kind {
	case token.NEWLINE, token.DEDENT, token.INDENT, token.EOF:
		return true
	}
	return p.lineDone
}

// ---------------------------------------------------------------------------
// Errors

func (p *parser) error(r source.Range, spec *diag.Spec, args ...any) *diag.Diagnostic {
	e := p.errs
	if r.Start > e.lastPos {
		e.lastPos = r.Start
	}
	if e.stopped {
		return &diag.Diagnostic{Spec: spec, Range: r, Args: args}
	}
	line := e.file.Line(r.Start)
	if e.lines[line] {
		// This line already has an error: a second one is most likely its consequence.
		return &diag.Diagnostic{Spec: spec, Range: r, Args: args}
	}
	e.lines[line] = true
	if e.count++; e.count > MaxErrors {
		e.stopped = true
		return e.bag.Add(diag.E0013, r)
	}
	return e.bag.Add(spec, r, args...)
}

// missing returns a position for a node absent from the source: right after
// the last token read, so that it stays inside its parent.
func (p *parser) missing() source.Pos { return p.prevEnd }

func (p *parser) tokRange() source.Range {
	return source.Range{Start: p.tok.Pos, End: p.tok.End}
}

func (p *parser) errorExpected(what string) *diag.Diagnostic {
	r := p.tokRange()
	switch p.tok.Kind {
	case token.NEWLINE, token.INDENT, token.DEDENT, token.EOF:
		// The line end is shown right after the last token, not after a comment.
		r = source.Range{Start: p.prevEnd, End: p.prevEnd}
	}
	return p.error(r, diag.E0012, what, describe(p.tok))
}

// expect reads a token of kind k or reports an error. It returns the
// token's position; on error, the position of the current token, which is not read.
func (p *parser) expect(k token.Kind) source.Pos {
	pos := p.tok.Pos
	if p.tok.Kind != k {
		p.errorExpected(quote(k.String()))
		return pos
	}
	p.next()
	return pos
}

// expectClose reads closing bracket k. If it is missing, the position is
// the end of the last token read so that nodes stay nested.
func (p *parser) expectClose(k token.Kind) source.Pos {
	if p.tok.Kind == k {
		pos := p.tok.Pos
		p.next()
		return pos
	}
	p.errorExpected(quote(k.String()))
	return p.prevEnd - 1
}

// expectNewline finishes a logical line. Extra tokens are skipped with one error.
func (p *parser) expectNewline() {
	if p.lineDone {
		p.lineDone = false
		return
	}
	switch p.tok.Kind {
	case token.NEWLINE:
		p.next()
		return
	case token.EOF, token.DEDENT:
		return
	}
	p.errorExpected(wantNewline)
	p.skipToNewline()
}

// skipToNewline skips tokens up to and including the end of the logical line.
func (p *parser) skipToNewline() {
	for !p.at(token.NEWLINE) && !p.at(token.EOF) && !p.at(token.DEDENT) && !p.at(token.INDENT) {
		p.next()
	}
	if p.at(token.NEWLINE) {
		p.next()
	}
}

// skipBlock skips a nested block starting at INDENT.
func (p *parser) skipBlock() {
	depth := 0
	for !p.at(token.EOF) {
		switch p.tok.Kind {
		case token.INDENT:
			depth++
		case token.DEDENT:
			depth--
		}
		p.next()
		if depth == 0 {
			return
		}
	}
}

// syncLine skips the rest of the line and its nested block.
func (p *parser) syncLine() {
	p.lineDone = false
	if p.at(token.DEDENT) {
		return
	}
	p.skipToNewline()
	if p.at(token.INDENT) {
		p.skipBlock()
	}
}

// unexpectedIndent reports an extra indent and skips the block.
func (p *parser) unexpectedIndent() {
	p.error(p.tokRange(), diag.E0015)
	p.skipBlock()
}

// ---------------------------------------------------------------------------
// Documentation

func (p *parser) collectDocs(comments []token.Comment) {
	p.leadDocs = map[int]*ast.CommentGroup{}
	p.lineDocs = map[int]*ast.CommentGroup{}
	var (
		group    *ast.CommentGroup
		lastLine int
	)
	flush := func() {
		if group != nil {
			p.leadDocs[lastLine+1] = group
			group = nil
		}
	}
	for _, c := range comments {
		if !c.Doc {
			continue
		}
		pos := p.file.Position(c.Pos)
		ownLine := true
		src := p.file.Source()
		for off := p.file.Offset(p.file.LineStart(pos.Line)); off < pos.Offset; off++ {
			if src[off] != ' ' && src[off] != '\t' && src[off] != '\r' {
				ownLine = false
				break
			}
		}
		if !ownLine {
			flush()
			p.lineDocs[pos.Line] = &ast.CommentGroup{List: []token.Comment{c}}
			continue
		}
		if group != nil && pos.Line != lastLine+1 {
			flush()
		}
		if group == nil {
			group = &ast.CommentGroup{}
		}
		group.List = append(group.List, c)
		lastLine = pos.Line
	}
	flush()
}

// doc returns the documentation of a declaration that starts at pos: "##"
// above it or at the end of the same line.
func (p *parser) doc(pos source.Pos) *ast.CommentGroup {
	line := p.file.Line(pos)
	if g := p.leadDocs[line]; g != nil {
		return g
	}
	return p.lineDocs[line]
}

// ---------------------------------------------------------------------------
// Common parts

func isUpper(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// parseIdent reads a name. A reserved word in place of a name is an error,
// but it is still read as a name so parsing can continue.
func (p *parser) parseIdent() *ast.Ident {
	switch {
	case p.at(token.IDENT):
		id := &ast.Ident{NamePos: p.tok.Pos, Name: p.tok.Text}
		p.next()
		return id
	case p.tok.Kind.IsKeyword():
		p.errorExpected(wantName).Note(source.Range{}, noteReserved, p.tok.Text)
		id := &ast.Ident{NamePos: p.tok.Pos, Name: p.tok.Text}
		p.next()
		return id
	}
	p.errorExpected(wantName)
	return &ast.Ident{NamePos: p.missing()} // an empty zero-length name
}

// parsePath reads a dotted path: car.plate.
func (p *parser) parsePath() *ast.Path {
	path := &ast.Path{Elems: []*ast.Ident{p.parseIdent()}}
	for p.at(token.DOT) && p.peek(1).Kind == token.IDENT {
		p.next()
		path.Elems = append(path.Elems, p.parseIdent())
	}
	return path
}

func (p *parser) parseAccess() *ast.AccessList {
	if !p.at(token.LPAREN) {
		return nil
	}
	a := &ast.AccessList{Lparen: p.tok.Pos}
	p.next()
	for !p.at(token.RPAREN) && !p.atLineEnd() {
		a.Names = append(a.Names, p.parseDottedName())
		if !p.at(token.COMMA) {
			break
		}
		p.next()
	}
	a.Rparen = p.expectClose(token.RPAREN)
	return a
}

// parseDottedName reads a name that may belong to another system: role
// Auth.admin, flag Auth.newLogin — one dotted name (RFC §23,
// decision 18).
func (p *parser) parseDottedName() *ast.Ident {
	id := p.parseIdent()
	for p.at(token.DOT) && p.tok.Pos == id.End() && p.peek(1).Kind == token.IDENT && p.peek(1).Pos == p.tok.Pos+1 {
		p.next()
		part := p.parseIdent()
		id = &ast.Ident{NamePos: id.NamePos, Name: id.Name + "." + part.Name}
	}
	return id
}

func (p *parser) parseParams() *ast.ParamList {
	if !p.at(token.LPAREN) {
		p.errorExpected(quote("("))
		return &ast.ParamList{Lparen: p.missing(), Rparen: p.missing() - 1}
	}
	l := &ast.ParamList{Lparen: p.tok.Pos}
	p.next()
	for !p.at(token.RPAREN) && !p.atLineEnd() {
		l.List = append(l.List, p.parseParam())
		if !p.at(token.COMMA) {
			break
		}
		p.next()
	}
	l.Rparen = p.expectClose(token.RPAREN)
	return l
}

func (p *parser) parseParam() *ast.Param {
	prm := &ast.Param{Name: p.parseIdent()}
	prm.Type = p.parseType()
	if p.at(token.ASSIGN) {
		p.next()
		prm.Default = p.parseExpr()
	}
	if p.atWord("personal") {
		prm.Personal = p.parsePersonal()
	}
	prm.Was = p.parseWas()
	return prm
}

// parseWas reads "was oldName" or returns nil.
func (p *parser) parseWas() *ast.Ident {
	if !p.atWord("was") || p.peek(1).Kind != token.IDENT {
		return nil
	}
	p.next()
	return p.parseIdent()
}

func (p *parser) parsePersonal() *ast.Personal {
	pers := &ast.Personal{PersonalPos: p.tok.Pos}
	p.next()
	if p.at(token.LPAREN) {
		p.next()
		pers.Subject = p.parsePath()
		pers.Rparen = p.expectClose(token.RPAREN)
	}
	return pers
}

func (p *parser) parseSortKeys() []*ast.SortKey {
	var keys []*ast.SortKey
	for {
		k := &ast.SortKey{Path: p.parsePath()}
		if p.atWord("desc") {
			k.DescPos = p.tok.Pos
			p.next()
		}
		keys = append(keys, k)
		if !p.at(token.COMMA) {
			return keys
		}
		p.next()
	}
}

func (p *parser) parseArgs() *ast.ArgList {
	if !p.at(token.LPAREN) {
		p.errorExpected(quote("("))
		return &ast.ArgList{Lparen: p.missing(), Rparen: p.missing() - 1}
	}
	l := &ast.ArgList{Lparen: p.tok.Pos}
	p.next()
	for !p.at(token.RPAREN) && !p.atLineEnd() {
		a := &ast.Arg{}
		if p.at(token.IDENT) && p.peek(1).Kind == token.COLON {
			a.Name = p.parseIdent()
			a.Colon = p.tok.Pos
			p.next()
		}
		a.Value = p.parseExpr()
		l.Args = append(l.Args, a)
		if !p.at(token.COMMA) {
			break
		}
		p.next()
	}
	l.Rparen = p.expectClose(token.RPAREN)
	return l
}

// ---------------------------------------------------------------------------
// Types

func (p *parser) parseType() ast.TypeExpr {
	var name *ast.Ident
	switch {
	case p.at(token.IDENT):
		name = p.parseIdent()
	case p.uiTypes && (p.at(token.ENUM) || p.at(token.VALUE) || p.at(token.SCREEN)):
		name = &ast.Ident{NamePos: p.tok.Pos, Name: p.tok.Text}
		p.next()
		return &ast.NamedType{Path: []*ast.Ident{name}}
	default:
		p.errorExpected(wantType)
		return &ast.BadType{From: p.missing(), To: p.missing()}
	}
	var t ast.TypeExpr
	if p.at(token.LT) {
		g := &ast.GenericType{Name: name, Lt: p.tok.Pos}
		p.next()
		for {
			g.Args = append(g.Args, p.parseType())
			if !p.at(token.COMMA) {
				break
			}
			p.next()
		}
		g.Gt = p.expectClose(token.GT)
		t = g
	} else {
		nt := &ast.NamedType{Path: []*ast.Ident{name}}
		for p.at(token.DOT) && p.peek(1).Kind == token.IDENT {
			p.next()
			nt.Path = append(nt.Path, p.parseIdent())
		}
		t = nt
	}
	if p.at(token.QUESTION) {
		t = &ast.OptionalType{Elem: t, Question: p.tok.Pos}
		p.next()
	}
	return t
}

// ---------------------------------------------------------------------------
// Blocks

// parseBlock reads an indented body after a heading. what names the
// heading (for an error message).
func (p *parser) parseBlock(header source.Range, what string) *ast.Block {
	if !p.at(token.INDENT) {
		p.missingBody(header, what)
		return &ast.Block{Start: p.prevEnd, EndPos: p.prevEnd}
	}
	p.next()
	b := &ast.Block{List: p.parseStmtList(), Start: p.prevEnd, EndPos: p.prevEnd}
	if len(b.List) > 0 {
		b.Start = b.List[0].Pos()
	}
	p.expectDedent()
	return b
}

func (p *parser) expectDedent() {
	if p.at(token.DEDENT) {
		p.next()
	}
}

// openBody checks that a block follows the heading and enters it.
func (p *parser) openBody(header source.Range, what string) bool {
	if !p.at(token.INDENT) {
		p.missingBody(header, what)
		return false
	}
	p.next()
	return true
}

// missingBody reports a missing body if the heading had no other errors:
// otherwise the body was most likely lost because of them.
func (p *parser) missingBody(header source.Range, what string) {
	if p.errs.lastPos >= header.Start {
		return
	}
	p.error(header, diag.E0016, what)
}

// blockLoop calls item for every line of a block up to DEDENT, making sure
// it advances and skipping extra indents.
func (p *parser) blockLoop(item func()) {
	for !p.at(token.DEDENT) && !p.at(token.EOF) {
		if p.at(token.INDENT) {
			p.unexpectedIndent()
			continue
		}
		before := p.i
		item()
		if p.i == before {
			p.syncLine()
		}
	}
}
