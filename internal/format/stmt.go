package format

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// stmts prints an indented body under a heading.
func (p *printer) stmts(b *ast.Block) doc { return body(p, b.List, p.stmt) }

func (p *printer) stmt(s ast.Stmt) doc {
	switch s := s.(type) {
	case *ast.TestCall:
		return p.testCall(s)
	case *ast.TimeOffset:
		return p.timeOffset(s)
	case *ast.LetStmt:
		d := cat(text("let "), p.ident(s.Name))
		if s.Type != nil {
			d = cat(d, text(" "), p.typ(s.Type))
		}
		return cat(d, text(" = "), p.expr(s.Value))
	case *ast.AssignStmt:
		return cat(p.expr(s.Target), text(" = "), p.expr(s.Value))
	case *ast.IfStmt:
		return p.ifStmt(s)
	case *ast.ForStmt:
		return cat(text("for "), p.ident(s.Var), text(" in "), p.expr(s.X), p.trailing(s.X.End()), p.stmts(s.Body))
	case *ast.WhileStmt:
		return cat(text("while "), p.expr(s.Cond), p.trailing(s.Cond.End()), p.stmts(s.Body))
	case *ast.BranchStmt:
		return text(s.Tok.String())
	case *ast.ReturnStmt:
		if s.Result == nil {
			return text("return")
		}
		return cat(text("return "), p.expr(s.Result))
	case *ast.ExprStmt:
		d := p.expr(s.X)
		if s.To != nil {
			d = cat(d, text(" to "), p.expr(s.To))
		}
		return d
	case *ast.DeleteStmt:
		return cat(text("delete "), p.expr(s.X))
	case *ast.ForgetStmt:
		return cat(text("forget "), p.expr(s.X))
	}
	panic("format: unexpected statement")
}

func (p *printer) ifStmt(s *ast.IfStmt) doc {
	d := cat(text("if "), p.expr(s.Cond), p.trailing(s.Cond.End()), p.stmts(s.Body))
	if s.Else == nil {
		return d
	}
	// Comments before else that do not belong to the body by indentation.
	d = cat(d, hardline, p.ownLines(s.ElsePos))
	switch e := s.Else.(type) {
	case *ast.IfStmt:
		return cat(d, text("else "), p.ifStmt(e))
	case *ast.Block:
		return cat(d, text("else"), p.trailing(s.ElsePos+source.Pos(len("else"))), p.stmts(e))
	}
	panic("format: unexpected else branch")
}
