package parser

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

func (p *parser) parseStmtList() []ast.Stmt {
	var list []ast.Stmt
	p.blockLoop(func() {
		list = append(list, p.parseStmt())
	})
	return list
}

func (p *parser) parseStmt() ast.Stmt {
	switch p.tok.Kind {
	case token.LET:
		return p.parseLet()
	case token.IF:
		return p.parseIf()
	case token.FOR:
		s := &ast.ForStmt{For: p.tok.Pos}
		p.next()
		s.Var = p.parseIdent()
		s.In = p.expect(token.IN)
		s.X = p.parseExpr()
		p.expectNewline()
		s.Body = p.parseBlock(source.Range{Start: s.For, End: s.X.End()}, whatFor)
		return s
	case token.WHILE:
		s := &ast.WhileStmt{While: p.tok.Pos}
		p.next()
		s.Cond = p.parseExpr()
		p.expectNewline()
		s.Body = p.parseBlock(source.Range{Start: s.While, End: s.Cond.End()}, whatWhile)
		return s
	case token.BREAK, token.CONTINUE:
		s := &ast.BranchStmt{TokPos: p.tok.Pos, Tok: p.tok.Kind}
		p.next()
		p.expectNewline()
		return s
	case token.RETURN:
		s := &ast.ReturnStmt{Return: p.tok.Pos}
		p.next()
		if !p.atLineEnd() {
			s.Result = p.parseExpr()
		}
		p.expectNewline()
		return s
	case token.DELETE:
		s := &ast.DeleteStmt{Delete: p.tok.Pos}
		p.next()
		s.X = p.parseExpr()
		p.expectNewline()
		return s
	case token.FORGET:
		s := &ast.ForgetStmt{Forget: p.tok.Pos}
		p.next()
		s.X = p.parseExpr()
		p.expectNewline()
		return s
	case token.ELSE, token.IN, token.AND, token.OR, token.IS,
		token.RPAREN, token.RBRACK, token.COMMA, token.DOT, token.COLON, token.ASSIGN:
		from := p.tok.Pos
		p.errorExpected(wantStmt)
		p.syncLine()
		return &ast.BadStmt{From: from, To: p.prevEnd}
	}
	if p.tok.Kind.IsKeyword() && !startsExpr(p.tok.Kind) {
		// A declaration inside a body: for example, a forgotten DEDENT or an extra indent.
		from := p.tok.Pos
		p.errorExpected(wantStmt)
		p.syncLine()
		return &ast.BadStmt{From: from, To: p.prevEnd}
	}
	return p.parseSimpleStmt()
}

// startsExpr reports whether a reserved word can start an expression.
func startsExpr(k token.Kind) bool {
	switch k {
	case token.NOT, token.NONE, token.TRUE, token.FALSE, token.NEW:
		return true
	}
	return false
}

func (p *parser) parseLet() *ast.LetStmt {
	s := &ast.LetStmt{Let: p.tok.Pos}
	p.next()
	s.Name = p.parseIdent()
	if !p.at(token.ASSIGN) {
		s.Type = p.parseType()
	}
	p.expect(token.ASSIGN)
	s.Value = p.parseExpr()
	p.expectNewline()
	return s
}

func (p *parser) parseIf() *ast.IfStmt {
	s := &ast.IfStmt{If: p.tok.Pos}
	p.next()
	s.Cond = p.parseExpr()
	p.expectNewline()
	s.Body = p.parseBlock(source.Range{Start: s.If, End: s.Cond.End()}, whatIf)
	if p.at(token.ELSE) {
		s.ElsePos = p.tok.Pos
		p.next()
		if p.at(token.IF) {
			s.Else = p.parseIf()
		} else {
			p.expectNewline()
			s.Else = p.parseBlock(source.Range{Start: s.ElsePos, End: s.ElsePos + 4}, whatElse)
		}
	}
	return s
}

// parseSimpleStmt parses an assignment or a call.
func (p *parser) parseSimpleStmt() ast.Stmt {
	if p.test > 0 && p.atTime() {
		return p.parseTimeOffset()
	}
	x := p.parseExpr()
	if p.test > 0 && !p.at(token.ASSIGN) {
		// In a test section a call is a step: a command or query [as who].
		c := &ast.TestCall{Call: x}
		if _, ok := x.(*ast.CallExpr); !ok && !isBad(x) {
			p.error(source.Range{Start: x.Pos(), End: x.End()}, diag.E0011)
		}
		if p.atWord("as") {
			c.AsPos = p.tok.Pos
			p.next()
			c.As = p.parseIdent()
		}
		p.expectNewline()
		return c
	}
	if p.at(token.ASSIGN) {
		s := &ast.AssignStmt{Target: x, Assign: p.tok.Pos}
		p.next()
		s.Value = p.parseExpr()
		if _, ok := x.(*ast.Ident); !ok && !isBad(x) {
			p.error(source.Range{Start: x.Pos(), End: x.End()}, diag.E0014)
		}
		p.expectNewline()
		return s
	}
	s := &ast.ExprStmt{X: x}
	if p.atWord("to") {
		s.ToPos = p.tok.Pos
		p.next()
		s.To = p.parseExpr()
	}
	if _, ok := x.(*ast.CallExpr); !ok && !isBad(x) {
		d := p.error(source.Range{Start: x.Pos(), End: x.End()}, diag.E0011)
		if b, ok := x.(*ast.BinaryExpr); ok && b.Op == token.EQ {
			d.Hint(hintAssign)
		}
	}
	p.expectNewline()
	return s
}

func isBad(x ast.Expr) bool {
	_, ok := x.(*ast.BadExpr)
	return ok
}
