package parser

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
	"strings"
)

// parseTest parses an acceptance test:
//
//	test "Name"
//	  feature tips, Auth.newLogin
//	  let x = ...
//	  given
//	    ...
//	  when
//	    ...
//	  then
//	    ...
func (p *parser) parseTest(doc *ast.CommentGroup) *ast.TestDecl {
	d := &ast.TestDecl{Doc: doc, Test: p.tok.Pos}
	p.next()
	d.Name = p.parseStringLit()
	p.expectNewline()
	if p.openBody(source.Range{Start: d.Test, End: d.Name.End()}, whatTest) {
		stage := 0
		p.blockLoop(func() { p.parseTestItem(d, &stage) })
		p.expectDedent()
	}
	d.EndPos = p.prevEnd
	return d
}

// testOrder is the order of test parts.
var testOrder = map[string]int{"let": 0, "given": 1, "when": 2, "then": 3}

func (p *parser) parseTestItem(d *ast.TestDecl, stage *int) {
	if p.atWord("feature") && p.peek(1).Kind == token.IDENT {
		p.parseTestFeatures(d, *stage)
		return
	}
	word := p.tok.Text
	if p.at(token.LET) {
		word = "let"
	} else if !p.at(token.IDENT) || p.peek(1).Kind != token.NEWLINE {
		p.errorExpected(wantTestItem)
		p.syncLine()
		return
	}
	order, ok := testOrder[word]
	if !ok {
		p.errorExpected(wantTestItem)
		p.syncLine()
		return
	}
	header := p.tokRange()
	switch {
	case order == *stage && order > 0:
		p.error(header, diag.E0018, word)
		p.syncLine()
		return
	case order < *stage:
		p.error(header, diag.E0020, word)
		p.syncLine()
		return
	}
	*stage = order

	pos := p.tok.Pos
	switch word {
	case "let":
		d.Setup = append(d.Setup, p.parseLet())
	case "given":
		p.next()
		p.next()
		b := &ast.GivenBlock{Given: pos}
		b.Steps = p.parseSteps(header)
		b.EndPos = p.prevEnd
		d.Given = b
	case "when":
		p.next()
		p.next()
		b := &ast.WhenBlock{When: pos}
		b.Steps = p.parseSteps(header)
		b.EndPos = p.prevEnd
		d.When = b
	case "then":
		p.next()
		p.next()
		b := &ast.ThenBlock{Then: pos}
		if p.openBody(header, whatSection) {
			p.blockLoop(func() { b.Items = append(b.Items, p.parseThenItem()) })
			p.expectDedent()
		}
		b.EndPos = p.prevEnd
		d.Then = b
	}
}

// parseTestFeatures reads the feature line: it is the first in a test.
func (p *parser) parseTestFeatures(d *ast.TestDecl, stage int) {
	f := &ast.TestFeatures{Feature: p.tok.Pos}
	header := p.tokRange()
	p.next()
	for {
		f.Names = append(f.Names, p.parseDottedName())
		if !p.at(token.COMMA) {
			break
		}
		p.next()
	}
	switch {
	case d.Features != nil:
		p.error(header, diag.E0018, "feature")
	case stage > 0 || len(d.Setup) > 0:
		p.error(header, diag.E0020, "feature")
	default:
		d.Features = f
	}
	p.expectNewline()
}

func (p *parser) parseSteps(header source.Range) []ast.TestStep {
	if !p.openBody(header, whatSection) {
		return nil
	}
	var steps []ast.TestStep
	p.test++
	p.blockLoop(func() {
		switch {
		case p.atTime():
			steps = append(steps, p.parseTimeOffset())
		case p.at(token.LET), p.at(token.FOR), p.at(token.WHILE), p.at(token.IF):
			steps = append(steps, &ast.TestStmt{Stmt: p.parseStmt()})
		case p.at(token.IDENT) && p.peek(1).Kind == token.ASSIGN:
			steps = append(steps, &ast.TestStmt{Stmt: p.parseSimpleStmt()})
		default:
			steps = append(steps, p.parseTestCall())
			p.expectNewline()
		}
	})
	p.test--
	p.expectDedent()
	return steps
}

// atTime reports whether the line starts with time offset or time passes.
func (p *parser) atTime() bool {
	return p.atWord("time") && (isWord(p.peek(1), "offset") || isWord(p.peek(1), "passes"))
}

// parseTimeOffset parses time offset ±d or the old time passes d.
func (p *parser) parseTimeOffset() *ast.TimeOffset {
	s := &ast.TimeOffset{Time: p.tok.Pos}
	p.next()
	passes := p.tok.Text == "passes"
	p.next()
	if !passes && p.at(token.ADD) {
		p.next() // +d is the same as d
	}
	s.Duration = p.parseExpr()
	p.expectNewline()
	return s
}

// parseTestCall parses a call [as who].
func (p *parser) parseTestCall() *ast.TestCall {
	c := &ast.TestCall{Call: p.parseExpr()}
	if _, ok := c.Call.(*ast.CallExpr); !ok && !isBad(c.Call) {
		p.error(source.Range{Start: c.Call.Pos(), End: c.Call.End()}, diag.E0011)
	}
	if p.atWord("as") {
		c.AsPos = p.tok.Pos
		p.next()
		c.As = p.parseIdent()
	}
	return c
}

func (p *parser) parseThenItem() ast.ThenItem {
	if strings.HasSuffix(p.file.Name(), ".perf.rr") {
		// A performance test: a check is a boolean expression over stats.
		t := &ast.ThenExpr{X: p.parseExpr()}
		p.expectNewline()
		return t
	}
	switch {
	case p.at(token.NOTIFICATION):
		t := &ast.ThenNotification{Notification: p.tok.Pos}
		p.next()
		t.Name = p.parseIdent()
		if p.at(token.LPAREN) {
			t.Args = p.parseArgs()
		}
		if p.atWord("to") {
			t.ToPos = p.tok.Pos
			p.next()
			t.To = p.parseExpr()
		}
		p.expectNewline()
		return t
	case p.at(token.ERROR):
		t := &ast.ThenError{Error: p.tok.Pos}
		p.next()
		t.Name = p.parseIdent()
		if p.at(token.LPAREN) {
			t.Args = p.parseArgs()
		}
		p.expectNewline()
		return t
	case p.at(token.REQUEST):
		t := &ast.ThenRequest{Request: p.tok.Pos}
		p.next()
		t.System = p.parseIdent()
		p.expect(token.DOT)
		t.Name = p.parseIdent()
		if p.at(token.LPAREN) {
			t.Args = p.parseArgs()
		}
		p.expectNewline()
		return t
	case p.atWord("matches") && p.peek(1).Kind == token.LPAREN:
		t := &ast.ThenMatches{Matches: p.tok.Pos}
		p.next()
		t.Fields = p.parseArgs()
		p.expectNewline()
		return t
	}
	from := p.tok.Pos
	call := p.parseTestCall()
	if !p.atWord("matches") {
		p.errorExpected(wantMatches)
		p.syncLine()
		return &ast.BadStmt{From: from, To: p.prevEnd}
	}
	t := &ast.ThenMatches{Call: call, Matches: p.tok.Pos}
	p.next()
	t.Fields = p.parseArgs()
	p.expectNewline()
	return t
}
