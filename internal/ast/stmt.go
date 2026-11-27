package ast

import (
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// Block is an indented block of statements.
type Block struct {
	Start  source.Pos
	List   []Stmt
	EndPos source.Pos
}

// LetStmt is let name [Type] = value.
type LetStmt struct {
	Let   source.Pos
	Name  *Ident
	Type  TypeExpr // or nil
	Value Expr
}

// AssignStmt is name = value.
type AssignStmt struct {
	Target Expr // *Ident; anything else is error E0014
	Assign source.Pos
	Value  Expr
}

// IfStmt — if / else if / else.
type IfStmt struct {
	If      source.Pos
	Cond    Expr
	Body    *Block
	ElsePos source.Pos
	Else    Stmt // *IfStmt, *Block or nil
}

// ForStmt is for x in collection.
type ForStmt struct {
	For  source.Pos
	Var  *Ident
	In   source.Pos
	X    Expr
	Body *Block
}

// WhileStmt is while condition.
type WhileStmt struct {
	While source.Pos
	Cond  Expr
	Body  *Block
}

// BranchStmt is break or continue.
type BranchStmt struct {
	TokPos source.Pos
	Tok    token.Kind
}

// ReturnStmt is return [value].
type ReturnStmt struct {
	Return source.Pos
	Result Expr // or nil
}

// ExprStmt is a call as a statement: an event, a command, an error, a
// notification (with an optional "to" recipient), a projection upsert, an
// external request.
type ExprStmt struct {
	X     Expr
	ToPos source.Pos
	To    Expr // or nil
}

// DeleteStmt is delete timer or delete [projection row].
type DeleteStmt struct {
	Delete source.Pos
	X      Expr
}

// ForgetStmt is forget subject.
type ForgetStmt struct {
	Forget source.Pos
	X      Expr
}

// BadStmt is a statement that could not be parsed.
type BadStmt struct {
	From, To source.Pos
}

func (s *Block) Pos() source.Pos      { return s.Start }
func (s *LetStmt) Pos() source.Pos    { return s.Let }
func (s *AssignStmt) Pos() source.Pos { return s.Target.Pos() }
func (s *IfStmt) Pos() source.Pos     { return s.If }
func (s *ForStmt) Pos() source.Pos    { return s.For }
func (s *WhileStmt) Pos() source.Pos  { return s.While }
func (s *BranchStmt) Pos() source.Pos { return s.TokPos }
func (s *ReturnStmt) Pos() source.Pos { return s.Return }
func (s *ExprStmt) Pos() source.Pos   { return s.X.Pos() }
func (s *DeleteStmt) Pos() source.Pos { return s.Delete }
func (s *ForgetStmt) Pos() source.Pos { return s.Forget }
func (s *BadStmt) Pos() source.Pos    { return s.From }

func (s *Block) End() source.Pos      { return s.EndPos }
func (s *LetStmt) End() source.Pos    { return s.Value.End() }
func (s *AssignStmt) End() source.Pos { return s.Value.End() }
func (s *IfStmt) End() source.Pos {
	if s.Else != nil {
		return s.Else.End()
	}
	return s.Body.End()
}
func (s *ForStmt) End() source.Pos   { return s.Body.End() }
func (s *WhileStmt) End() source.Pos { return s.Body.End() }
func (s *BranchStmt) End() source.Pos {
	return s.TokPos + source.Pos(len(s.Tok.String()))
}
func (s *ReturnStmt) End() source.Pos {
	if s.Result != nil {
		return s.Result.End()
	}
	return s.Return + source.Pos(len("return"))
}
func (s *ExprStmt) End() source.Pos {
	if s.To != nil {
		return s.To.End()
	}
	return s.X.End()
}
func (s *DeleteStmt) End() source.Pos { return s.X.End() }
func (s *ForgetStmt) End() source.Pos { return s.X.End() }
func (s *BadStmt) End() source.Pos    { return s.To }

func (*Block) stmtNode()      {}
func (*LetStmt) stmtNode()    {}
func (*AssignStmt) stmtNode() {}
func (*IfStmt) stmtNode()     {}
func (*ForStmt) stmtNode()    {}
func (*WhileStmt) stmtNode()  {}
func (*BranchStmt) stmtNode() {}
func (*ReturnStmt) stmtNode() {}
func (*ExprStmt) stmtNode()   {}
func (*DeleteStmt) stmtNode() {}
func (*ForgetStmt) stmtNode() {}
func (*BadStmt) stmtNode()    {}

// BadStmt can also stand in place of test steps and checks.
func (*BadStmt) testStepNode() {}
func (*BadStmt) thenItemNode() {}
