package ast

import "github.com/tycheg/reasonr-compiler/internal/source"

// AppDecl is an app (RFC §22.2): app(access) Name "Title" [auto], followed
// by language, platforms, default and the root layout (usually a menu).
type AppDecl struct {
	Doc     *CommentGroup
	App     source.Pos
	Access  *AccessList
	Name    *Ident
	Title   *BasicLit  // or nil
	AutoPos source.Pos // NoPos when the layout is written
	Items   []AppItem
	EndPos  source.Pos
}

// IsAuto reports whether the app's UI is built automatically.
func (d *AppDecl) IsAuto() bool { return d.AutoPos.IsValid() }

// AppItem is a line of an app: language, platforms, default or a layout node.
type AppItem interface {
	Node
	appItemNode()
}

// LanguageItem is language en: the base language of the app's layout strings.
type LanguageItem struct {
	Language source.Pos
	Name     *Ident
}

// PlatformsItem — platforms web, mobile.
type PlatformsItem struct {
	Platforms source.Pos
	Names     []*Ident
}

// DefaultDecl is the default primitive for a type: default Type
// Primitive(parameters). It stands at the top level (the whole module) or
// in an app. The type may be a constraint — id, enum, value, set<enum>:
// that is a NamedType with that name.
type DefaultDecl struct {
	Doc     *CommentGroup
	Default source.Pos
	Type    TypeExpr
	Prim    *Ident
	Args    *ArgList // or nil
}

// ScreenDecl is a screen (RFC §22.3): screen Name(parameters) "Title" and a layout.
type ScreenDecl struct {
	Doc    *CommentGroup
	Screen source.Pos
	Name   *Ident
	Params *ParamList // or nil
	Title  *BasicLit  // or nil
	Nodes  []UINode
	EndPos source.Pos
}

// UINode is a layout line: a primitive, a part or a screen (PrimNode), a
// field (FieldNode), a transition (OnNode).
type UINode interface {
	Node
	uiNode()
}

// PrimNode is a layout line starting with a capital letter: a primitive
// with parameters and a binding, a part of a primitive, or a screen
// (embedding, a navigation item). The check decides which: names are
// globally unique.
type PrimNode struct {
	Name     *Ident
	Args     *ArgList // primitive parameters or screen arguments; nil
	Binding  *Binding // or nil
	Label    *BasicLit
	IfPos    source.Pos
	Cond     Expr // or nil
	Children []UINode
	EndPos   source.Pos
}

// Binding is a primitive binding: Type.Method(arguments),
// [target].Command(arguments) or Screen(arguments).
type Binding struct {
	Target *BracketExpr // [target] or nil
	Type   *Ident       // the method's type or the screen; nil when Target is set
	Dot    source.Pos   // NoPos for a screen
	Method *Ident       // nil for a screen
	Args   *ArgList     // or nil
}

// IsScreen reports whether the primitive is bound to a screen.
func (b *Binding) IsScreen() bool { return b.Method == nil }

// FieldNode is a layout line starting with a lowercase letter: a data field
// or a parameter input; the primitive is optional.
type FieldNode struct {
	Name     *Ident
	Prim     *Ident   // or nil for the type's default primitive
	PrimArgs *ArgList // or nil
	Label    *BasicLit
	IfPos    source.Pos
	Cond     Expr // or nil
	Children []UINode
	EndPos   source.Pos
}

// OnNode is a transition on a primitive event: on event Screen(arguments)
// or on event back.
type OnNode struct {
	On      source.Pos
	Event   *Ident
	Screen  *Ident     // nil — back
	Args    *ArgList   // or nil
	BackPos source.Pos // NoPos when the transition goes to a screen
}

// IsBack reports whether the transition returns to the previous screen.
func (n *OnNode) IsBack() bool { return n.Screen == nil }

// BadNode is a layout or app line that could not be parsed.
type BadNode struct {
	From, To source.Pos
}

func (d *AppDecl) Pos() source.Pos       { return d.App }
func (d *AppDecl) End() source.Pos       { return d.EndPos }
func (d *DefaultDecl) Pos() source.Pos   { return d.Default }
func (d *ScreenDecl) Pos() source.Pos    { return d.Screen }
func (d *ScreenDecl) End() source.Pos    { return d.EndPos }
func (i *LanguageItem) Pos() source.Pos  { return i.Language }
func (i *LanguageItem) End() source.Pos  { return i.Name.End() }
func (i *PlatformsItem) Pos() source.Pos { return i.Platforms }
func (i *PlatformsItem) End() source.Pos {
	if len(i.Names) == 0 {
		return i.Platforms + source.Pos(len("platforms"))
	}
	return i.Names[len(i.Names)-1].End()
}
func (d *DefaultDecl) End() source.Pos {
	switch {
	case d.Args != nil:
		return d.Args.End()
	case d.Prim != nil:
		return d.Prim.End()
	}
	return d.Type.End()
}

func (n *PrimNode) Pos() source.Pos  { return n.Name.Pos() }
func (n *PrimNode) End() source.Pos  { return n.EndPos }
func (n *FieldNode) Pos() source.Pos { return n.Name.Pos() }
func (n *FieldNode) End() source.Pos { return n.EndPos }
func (n *OnNode) Pos() source.Pos    { return n.On }
func (n *OnNode) End() source.Pos {
	switch {
	case n.Args != nil:
		return n.Args.End()
	case n.Screen != nil:
		return n.Screen.End()
	case n.BackPos.IsValid():
		return n.BackPos + source.Pos(len("back"))
	}
	return n.Event.End()
}
func (n *BadNode) Pos() source.Pos { return n.From }
func (n *BadNode) End() source.Pos { return n.To }

func (b *Binding) Pos() source.Pos {
	if b.Target != nil {
		return b.Target.Pos()
	}
	return b.Type.Pos()
}
func (b *Binding) End() source.Pos {
	switch {
	case b.Args != nil:
		return b.Args.End()
	case b.Method != nil:
		return b.Method.End()
	}
	return b.Type.End()
}

func (*AppDecl) declNode()     {}
func (*DefaultDecl) declNode() {}
func (*ScreenDecl) declNode()  {}

func (*LanguageItem) appItemNode()  {}
func (*PlatformsItem) appItemNode() {}
func (*DefaultDecl) appItemNode()   {}
func (*PrimNode) appItemNode()      {}
func (*FieldNode) appItemNode()     {}
func (*OnNode) appItemNode()        {}
func (*BadNode) appItemNode()       {}

func (*PrimNode) uiNode()  {}
func (*FieldNode) uiNode() {}
func (*OnNode) uiNode()    {}
func (*BadNode) uiNode()   {}
