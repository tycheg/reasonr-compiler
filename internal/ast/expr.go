package ast

import (
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// BasicLit is a literal: a number, a duration, a string without
// interpolation, a date, a time, true, false, none.
type BasicLit struct {
	ValuePos source.Pos
	Kind     token.Kind // INT, DECIMAL, DURATION, STRING, DATE, TIMESTAMP, TRUE, FALSE, NONE
	Raw      string     // source text
	Value    string     // for STRING the decoded text, for DATE/TIMESTAMP the content of the quotes
}

// InterpString is a string with interpolation: "Order {orderId}".
type InterpString struct {
	Quote  source.Pos
	Parts  []InterpPart
	EndPos source.Pos
}

// InterpPart is a part of a string: text (X == nil) or an expression.
type InterpPart struct {
	Text string
	X    Expr
}

// ParenExpr is an expression in parentheses.
type ParenExpr struct {
	Lparen source.Pos
	X      Expr
	Rparen source.Pos
}

// UnaryExpr is -x or not x.
type UnaryExpr struct {
	OpPos source.Pos
	Op    token.Kind // SUB or NOT
	X     Expr
}

// BinaryExpr is a binary operation: arithmetic, comparisons, ??, and, or.
type BinaryExpr struct {
	X     Expr
	OpPos source.Pos
	Op    token.Kind
	Y     Expr
}

// IsNoneExpr is x is none or x is not none.
type IsNoneExpr struct {
	X      Expr
	Is     source.Pos
	Not    bool
	EndPos source.Pos
}

// InExpr is x in collection or x not in collection.
type InExpr struct {
	X     Expr
	OpPos source.Pos // position of not or in
	Not   bool
	Y     Expr
}

// SelectorExpr is x.name or x?.name.
type SelectorExpr struct {
	X    Expr
	Dot  source.Pos
	Safe bool // ?.
	Sel  *Ident
}

// CallExpr is a call: f(a: 1), [x].cmd(...), Type(...), x.method(...).
type CallExpr struct {
	Fun  Expr
	Args *ArgList
}

// BracketExpr is [a, b, …]. Whether it is an entity access ([x]), a search
// by type ([Order] without sections) or a list literal is decided by type checking.
type BracketExpr struct {
	Lbrack source.Pos
	Elems  []Expr
	Rbrack source.Pos
}

// MapLit is a map ["a": 1] or an empty [:].
type MapLit struct {
	Lbrack  source.Pos
	Entries []*MapEntry
	Rbrack  source.Pos
}

// MapEntry is a key: value pair.
type MapEntry struct {
	Key   Expr
	Colon source.Pos
	Value Expr
}

func (e *MapEntry) Pos() source.Pos { return e.Key.Pos() }
func (e *MapEntry) End() source.Pos { return e.Value.End() }

// SearchExpr is a search [Type] with where, sort, page or limit, select sections.
type SearchExpr struct {
	Lbrack    source.Pos
	Type      *Ident
	Rbrack    source.Pos
	WherePos  source.Pos
	Where     Expr // or nil
	SortPos   source.Pos
	Sort      []*SortKey
	PagePos   source.Pos
	Page      Expr // or nil
	LimitPos  source.Pos
	Limit     Expr // or nil
	SelectPos source.Pos
	Select    Expr // or nil
	EndPos    source.Pos
}

// NewIDExpr is new Order.Id; in layout arguments it is a bare new (Type ==
// nil): the type comes from the parameter.
type NewIDExpr struct {
	New  source.Pos
	Type *NamedType
}

// AllExpr is all Order (only in migrations).
type AllExpr struct {
	All  source.Pos
	Type *Ident
}

// BadExpr is an expression that could not be parsed.
type BadExpr struct {
	From, To source.Pos
}

func (x *BasicLit) Pos() source.Pos     { return x.ValuePos }
func (x *InterpString) Pos() source.Pos { return x.Quote }
func (x *ParenExpr) Pos() source.Pos    { return x.Lparen }
func (x *UnaryExpr) Pos() source.Pos    { return x.OpPos }
func (x *BinaryExpr) Pos() source.Pos   { return x.X.Pos() }
func (x *IsNoneExpr) Pos() source.Pos   { return x.X.Pos() }
func (x *InExpr) Pos() source.Pos       { return x.X.Pos() }
func (x *SelectorExpr) Pos() source.Pos { return x.X.Pos() }
func (x *CallExpr) Pos() source.Pos     { return x.Fun.Pos() }
func (x *BracketExpr) Pos() source.Pos  { return x.Lbrack }
func (x *MapLit) Pos() source.Pos       { return x.Lbrack }
func (x *SearchExpr) Pos() source.Pos   { return x.Lbrack }
func (x *NewIDExpr) Pos() source.Pos    { return x.New }
func (x *AllExpr) Pos() source.Pos      { return x.All }
func (x *BadExpr) Pos() source.Pos      { return x.From }

func (x *BasicLit) End() source.Pos     { return x.ValuePos + source.Pos(len(x.Raw)) }
func (x *InterpString) End() source.Pos { return x.EndPos }
func (x *ParenExpr) End() source.Pos    { return x.Rparen + 1 }
func (x *UnaryExpr) End() source.Pos    { return x.X.End() }
func (x *BinaryExpr) End() source.Pos   { return x.Y.End() }
func (x *IsNoneExpr) End() source.Pos   { return x.EndPos }
func (x *InExpr) End() source.Pos       { return x.Y.End() }
func (x *SelectorExpr) End() source.Pos { return x.Sel.End() }
func (x *CallExpr) End() source.Pos     { return x.Args.End() }
func (x *BracketExpr) End() source.Pos  { return x.Rbrack + 1 }
func (x *MapLit) End() source.Pos       { return x.Rbrack + 1 }
func (x *SearchExpr) End() source.Pos   { return x.EndPos }
func (x *NewIDExpr) End() source.Pos {
	if x.Type == nil {
		return x.New + source.Pos(len("new"))
	}
	return x.Type.End()
}
func (x *AllExpr) End() source.Pos { return x.Type.End() }
func (x *BadExpr) End() source.Pos { return x.To }

func (*Ident) exprNode()        {}
func (*BasicLit) exprNode()     {}
func (*InterpString) exprNode() {}
func (*ParenExpr) exprNode()    {}
func (*UnaryExpr) exprNode()    {}
func (*BinaryExpr) exprNode()   {}
func (*IsNoneExpr) exprNode()   {}
func (*InExpr) exprNode()       {}
func (*SelectorExpr) exprNode() {}
func (*CallExpr) exprNode()     {}
func (*BracketExpr) exprNode()  {}
func (*MapLit) exprNode()       {}
func (*SearchExpr) exprNode()   {}
func (*NewIDExpr) exprNode()    {}
func (*AllExpr) exprNode()      {}
func (*BadExpr) exprNode()      {}

// ---------------------------------------------------------------------------
// Types

// NamedType is a type name or path: int, Order, Order.Id, Timer.Id.
type NamedType struct {
	Path []*Ident
}

// GenericType — list<T>, set<T>, page<T>, map<K, V>.
type GenericType struct {
	Name *Ident
	Lt   source.Pos
	Args []TypeExpr
	Gt   source.Pos
}

// OptionalType — T?.
type OptionalType struct {
	Elem     TypeExpr
	Question source.Pos
}

// BadType is a type that could not be parsed.
// UnionType is A | B: one of the types; only in primitive declarations.
type UnionType struct {
	Types []TypeExpr
}

func (t *UnionType) Pos() source.Pos { return t.Types[0].Pos() }
func (t *UnionType) End() source.Pos { return t.Types[len(t.Types)-1].End() }
func (*UnionType) typeNode()         {}

type BadType struct {
	From, To source.Pos
}

func (t *NamedType) Pos() source.Pos    { return t.Path[0].Pos() }
func (t *GenericType) Pos() source.Pos  { return t.Name.Pos() }
func (t *OptionalType) Pos() source.Pos { return t.Elem.Pos() }
func (t *BadType) Pos() source.Pos      { return t.From }

func (t *NamedType) End() source.Pos    { return t.Path[len(t.Path)-1].End() }
func (t *GenericType) End() source.Pos  { return t.Gt + 1 }
func (t *OptionalType) End() source.Pos { return t.Question + 1 }
func (t *BadType) End() source.Pos      { return t.To }

func (*NamedType) typeNode()    {}
func (*GenericType) typeNode()  {}
func (*OptionalType) typeNode() {}
func (*BadType) typeNode()      {}

// String returns the dotted type name: Order.Id.
func (t *NamedType) String() string {
	names := make([]string, len(t.Path))
	for i, e := range t.Path {
		names[i] = e.Name
	}
	return strings.Join(names, ".")
}

// TypeString returns the type in canonical form: map<string, Money>?.
func TypeString(t TypeExpr) string {
	switch t := t.(type) {
	case *NamedType:
		return t.String()
	case *GenericType:
		args := make([]string, len(t.Args))
		for i, a := range t.Args {
			args[i] = TypeString(a)
		}
		return t.Name.Name + "<" + strings.Join(args, ", ") + ">"
	case *OptionalType:
		return TypeString(t.Elem) + "?"
	case *UnionType:
		alts := make([]string, len(t.Types))
		for i, a := range t.Types {
			alts[i] = TypeString(a)
		}
		return strings.Join(alts, " | ")
	}
	return "?"
}
