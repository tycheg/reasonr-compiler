package ast

import "github.com/tycheg/reasonr-compiler/internal/source"

// GivenBlock is the given section.
type GivenBlock struct {
	Given  source.Pos
	Steps  []TestStep
	EndPos source.Pos
}

// WhenBlock is the when section; it must hold exactly one action (sema checks).
type WhenBlock struct {
	When   source.Pos
	Steps  []TestStep
	EndPos source.Pos
}

// ThenBlock is the then section.
type ThenBlock struct {
	Then   source.Pos
	Items  []ThenItem
	EndPos source.Pos
}

// TestCall is a command or query call in a test: call [as who].
type TestCall struct {
	Call  Expr // *CallExpr; anything else is an error
	AsPos source.Pos
	As    *Ident // or nil
}

// TimeOffset is time offset ±duration: a shift of the scenario clock (RFC
// §27.1). Duration is signed: "-30d" is a unary minus. The parser accepts
// the old form time passes d; reasonr fmt writes it as time offset +d.
type TimeOffset struct {
	Time     source.Pos
	Duration Expr
}

// TestStmt is a statement in a test section: let, assignment, for, while,
// if (RFC §27.5). Steps inside its blocks are TestCall
// and TimeOffset as statements.
type TestStmt struct {
	Stmt Stmt
}

// ThenNotification is notification name(arguments) [to whom].
type ThenNotification struct {
	Notification source.Pos
	Name         *Ident
	Args         *ArgList // or nil
	ToPos        source.Pos
	To           Expr // or nil
}

// ThenError is error name[(arguments)].
type ThenError struct {
	Error source.Pos
	Name  *Ident
	Args  *ArgList // or nil
}

// ThenRequest is request System.request(arguments).
type ThenRequest struct {
	Request source.Pos
	System  *Ident
	Name    *Ident
	Args    *ArgList // or nil
}

// ThenExpr is a performance test check (*.perf.rr, RFC §27.6): a
// boolean expression over stats and baseline, such as "stats.p95 < 100ms".
type ThenExpr struct {
	X Expr
}

// ThenMatches is [query call] matches (field: value, ...). Call == nil
// compares the result of the query from the when section.
type ThenMatches struct {
	Call    *TestCall
	Matches source.Pos
	Fields  *ArgList
}

func (b *GivenBlock) Pos() source.Pos { return b.Given }
func (b *WhenBlock) Pos() source.Pos  { return b.When }
func (b *ThenBlock) Pos() source.Pos  { return b.Then }
func (b *GivenBlock) End() source.Pos { return b.EndPos }
func (b *WhenBlock) End() source.Pos  { return b.EndPos }
func (b *ThenBlock) End() source.Pos  { return b.EndPos }

func (s *TestCall) Pos() source.Pos { return s.Call.Pos() }
func (s *TestCall) End() source.Pos {
	if s.As != nil {
		return s.As.End()
	}
	return s.Call.End()
}
func (s *TimeOffset) Pos() source.Pos { return s.Time }
func (s *TimeOffset) End() source.Pos { return s.Duration.End() }
func (s *TestStmt) Pos() source.Pos   { return s.Stmt.Pos() }
func (s *TestStmt) End() source.Pos   { return s.Stmt.End() }

func (t *ThenNotification) Pos() source.Pos { return t.Notification }
func (t *ThenNotification) End() source.Pos {
	switch {
	case t.To != nil:
		return t.To.End()
	case t.Args != nil:
		return t.Args.End()
	}
	return t.Name.End()
}
func (t *ThenError) Pos() source.Pos { return t.Error }
func (t *ThenError) End() source.Pos {
	if t.Args != nil {
		return t.Args.End()
	}
	return t.Name.End()
}
func (t *ThenRequest) Pos() source.Pos { return t.Request }
func (t *ThenRequest) End() source.Pos {
	if t.Args != nil {
		return t.Args.End()
	}
	return t.Name.End()
}
func (t *ThenMatches) Pos() source.Pos {
	if t.Call != nil {
		return t.Call.Pos()
	}
	return t.Matches
}
func (t *ThenMatches) End() source.Pos { return t.Fields.End() }

func (*TestCall) testStepNode()   {}
func (*TimeOffset) testStepNode() {}
func (*TestStmt) testStepNode()   {}

// Steps in blocks of test statements (a for body, if branches) are statements.
func (*TestCall) stmtNode()   {}
func (*TimeOffset) stmtNode() {}

func (*ThenNotification) thenItemNode() {}
func (*ThenError) thenItemNode()        {}
func (*ThenRequest) thenItemNode()      {}
func (*ThenExpr) thenItemNode()         {}

func (t *ThenExpr) Pos() source.Pos { return t.X.Pos() }
func (t *ThenExpr) End() source.Pos { return t.X.End() }
func (*ThenMatches) thenItemNode()  {}
