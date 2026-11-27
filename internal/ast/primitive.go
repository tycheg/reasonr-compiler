package ast

import "github.com/tycheg/reasonr-compiler/internal/source"

// PrimitiveDecl declares a UI primitive (RFC §22.7): what it binds to, its
// parameters, parts and events.
type PrimitiveDecl struct {
	Doc        *CommentGroup
	Primitive  source.Pos
	Name       *Ident
	TypeParams *TypeParamList // <R, K id> or nil
	Params     *PrimParamList // or nil
	Items      []PrimItem
	EndPos     source.Pos
}

// TypeParamList is the type variables of a primitive: <R, K id>.
type TypeParamList struct {
	Lt   source.Pos
	List []*TypeParam
	Gt   source.Pos
}

// TypeParam is a type variable and an optional constraint: id, enum, value.
type TypeParam struct {
	Name       *Ident
	Constraint *Ident // or nil
}

// PrimParamList is primitive parameters in parentheses.
type PrimParamList struct {
	Lparen source.Pos
	List   []*PrimParam
	Rparen source.Pos
}

// PrimParam is a primitive parameter: size int = 20, label field<R> = auto,
// or a query parameter search(text string) list<R>.
type PrimParam struct {
	Name    *Ident
	Query   *ParamList // values the primitive passes to the query; nil when not a query
	Type    TypeExpr
	Default Expr       // or nil
	AutoPos source.Pos // = auto
}

// IsAuto reports whether the compiler chooses the parameter value.
func (p *PrimParam) IsAuto() bool { return p.AutoPos.IsValid() }

// PrimItem is a line of a primitive declaration: binds, children, part, event.
type PrimItem interface {
	Node
	primItemNode()
}

// Binding kinds in binds: "" is a query result (the type in Type).
const (
	BindCommand = "command"
	BindScreen  = "screen"
	BindField   = "field"
	BindInput   = "input"
)

// BindsItem — binds page<Row>, binds command, binds field T, binds input K | set<K>.
type BindsItem struct {
	Binds   source.Pos
	KindPos source.Pos // NoPos for a query result
	Kind    string
	Type    TypeExpr // nil for command and screen
}

// What names in a part bind to: "" means fields of a value of type Type.
const (
	PartParams  = "params"
	PartInputs  = "inputs"
	PartScreens = "screens"
)

// PartItem is part Name [binds what] or children binds what.
type PartItem struct {
	Part     source.Pos // part or children
	Children bool
	Name     *Ident     // nil for children
	BindsPos source.Pos // NoPos for a part made of primitives only
	KindPos  source.Pos
	Kind     string   // "" with Type, or PartParams, PartInputs, PartScreens
	Type     TypeExpr // or nil
}

// PrimEvent is a primitive event: event open(row Row).
type PrimEvent struct {
	Event  source.Pos
	Name   *Ident
	Params *ParamList // or nil
}

func (d *PrimitiveDecl) Pos() source.Pos { return d.Primitive }
func (d *PrimitiveDecl) End() source.Pos { return d.EndPos }
func (l *TypeParamList) Pos() source.Pos { return l.Lt }
func (l *TypeParamList) End() source.Pos { return l.Gt + 1 }
func (t *TypeParam) Pos() source.Pos     { return t.Name.Pos() }
func (t *TypeParam) End() source.Pos {
	if t.Constraint != nil {
		return t.Constraint.End()
	}
	return t.Name.End()
}
func (l *PrimParamList) Pos() source.Pos { return l.Lparen }
func (l *PrimParamList) End() source.Pos { return l.Rparen + 1 }
func (p *PrimParam) Pos() source.Pos     { return p.Name.Pos() }
func (p *PrimParam) End() source.Pos {
	switch {
	case p.AutoPos.IsValid():
		return p.AutoPos + source.Pos(len("auto"))
	case p.Default != nil:
		return p.Default.End()
	case p.Type != nil:
		return p.Type.End()
	}
	return p.Name.End()
}
func (i *BindsItem) Pos() source.Pos { return i.Binds }
func (i *BindsItem) End() source.Pos {
	switch {
	case i.Type != nil:
		return i.Type.End()
	case i.KindPos.IsValid():
		return i.KindPos + source.Pos(len(i.Kind))
	}
	return i.Binds + source.Pos(len("binds"))
}
func (i *PartItem) Pos() source.Pos { return i.Part }
func (i *PartItem) End() source.Pos {
	switch {
	case i.Type != nil:
		return i.Type.End()
	case i.KindPos.IsValid():
		return i.KindPos + source.Pos(len(i.Kind))
	case i.BindsPos.IsValid():
		return i.BindsPos + source.Pos(len("binds"))
	case i.Name != nil:
		return i.Name.End()
	}
	return i.Part + source.Pos(len("children"))
}
func (e *PrimEvent) Pos() source.Pos { return e.Event }
func (e *PrimEvent) End() source.Pos {
	if e.Params != nil {
		return e.Params.End()
	}
	return e.Name.End()
}

func (*PrimitiveDecl) declNode() {}

func (*BindsItem) primItemNode() {}
func (*PartItem) primItemNode()  {}
func (*PrimEvent) primItemNode() {}
func (*BadNode) primItemNode()   {}
