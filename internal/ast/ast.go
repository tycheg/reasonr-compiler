// Package ast describes the ReasonR syntax tree.
//
// Nodes keep the positions of all significant tokens so the formatter and
// the LSP can restore the source text and diagnostics can point at the
// exact place. The meaning of constructs (for example, whether [a] is an
// entity access or a list) is decided by type checking, not the parser.
package ast

import (
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// Node is any tree node.
type Node interface {
	Pos() source.Pos // the first position of the node
	End() source.Pos // the position right after the node
}

// Decl is a top-level declaration.
type Decl interface {
	Node
	declNode()
}

// Member is a member of an entity, actor, projection, value object,
// external system or extension.
type Member interface {
	Node
	memberNode()
}

// Stmt is a statement.
type Stmt interface {
	Node
	stmtNode()
}

// Expr is an expression.
type Expr interface {
	Node
	exprNode()
}

// TypeExpr is a type as written.
type TypeExpr interface {
	Node
	typeNode()
}

// TestStep is a step of the given and when sections.
type TestStep interface {
	Node
	testStepNode()
}

// ThenItem is a check in the then section.
type ThenItem interface {
	Node
	thenItemNode()
}

// ---------------------------------------------------------------------------
// Comments

// CommentGroup is consecutive documentation comments "##".
type CommentGroup struct {
	List []token.Comment
}

func (g *CommentGroup) Pos() source.Pos { return g.List[0].Pos }
func (g *CommentGroup) End() source.Pos { return g.List[len(g.List)-1].End }

// Text returns the documentation text without "##" and the leading space.
func (g *CommentGroup) Text() string {
	if g == nil {
		return ""
	}
	lines := make([]string, len(g.List))
	for i, c := range g.List {
		t := strings.TrimPrefix(c.Text, "##")
		lines[i] = strings.TrimPrefix(t, " ")
	}
	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// File

// File is a parsed source file.
type File struct {
	Name      string
	FileStart source.Pos
	FileEnd   source.Pos
	Decls     []Decl
	Comments  []token.Comment // all comments of the file in order
}

func (f *File) Pos() source.Pos { return f.FileStart }
func (f *File) End() source.Pos { return f.FileEnd }

// ---------------------------------------------------------------------------
// Common parts of declarations

// Ident is a name.
type Ident struct {
	NamePos source.Pos
	Name    string
}

func (x *Ident) Pos() source.Pos { return x.NamePos }
func (x *Ident) End() source.Pos { return x.NamePos + source.Pos(len(x.Name)) }

func (x *Ident) String() string {
	if x == nil {
		return "<nil>"
	}
	return x.Name
}

// Path is a dotted path to a field: car.plate.
type Path struct {
	Elems []*Ident
}

func (p *Path) Pos() source.Pos { return p.Elems[0].Pos() }
func (p *Path) End() source.Pos { return p.Elems[len(p.Elems)-1].End() }

func (p *Path) String() string {
	names := make([]string, len(p.Elems))
	for i, e := range p.Elems {
		names[i] = e.Name
	}
	return strings.Join(names, ".")
}

// AccessList is access in parentheses: command(customer, support).
type AccessList struct {
	Lparen source.Pos
	Names  []*Ident
	Rparen source.Pos
}

func (a *AccessList) Pos() source.Pos { return a.Lparen }
func (a *AccessList) End() source.Pos { return a.Rparen + 1 }

// ParamList is parameters in parentheses.
type ParamList struct {
	Lparen source.Pos
	List   []*Param
	Rparen source.Pos
}

func (l *ParamList) Pos() source.Pos { return l.Lparen }
func (l *ParamList) End() source.Pos { return l.Rparen + 1 }

// Param is a parameter: name Type [= value] [personal[(path)]].
type Param struct {
	Name     *Ident
	Type     TypeExpr
	Default  Expr      // or nil
	Personal *Personal // or nil
	// Was is the old name of the parameter (cancelReason string was reason):
	// clients of earlier API versions pass the argument by it
	// (RFC §31.3.1); nil when none.
	Was *Ident
}

func (p *Param) Pos() source.Pos { return p.Name.Pos() }
func (p *Param) End() source.Pos {
	switch {
	case p.Was != nil:
		return p.Was.End()
	case p.Personal != nil:
		return p.Personal.End()
	case p.Default != nil:
		return p.Default.End()
	case p.Type != nil:
		return p.Type.End()
	}
	return p.Name.End()
}

// Personal is the personal or personal(subject) attribute.
type Personal struct {
	PersonalPos source.Pos
	Subject     *Path      // nil: the subject is the entity itself
	Rparen      source.Pos // NoPos when there is no subject
}

func (p *Personal) Pos() source.Pos { return p.PersonalPos }
func (p *Personal) End() source.Pos {
	if p.Rparen.IsValid() {
		return p.Rparen + 1
	}
	return p.PersonalPos + source.Pos(len("personal"))
}

// SortKey is an index or sort key: path [desc].
type SortKey struct {
	Path    *Path
	DescPos source.Pos // NoPos for ascending
}

func (k *SortKey) Pos() source.Pos { return k.Path.Pos() }
func (k *SortKey) End() source.Pos {
	if k.DescPos.IsValid() {
		return k.DescPos + source.Pos(len("desc"))
	}
	return k.Path.End()
}

// Desc reports whether descending order is set.
func (k *SortKey) Desc() bool { return k.DescPos.IsValid() }

// ArgList is call arguments in parentheses.
type ArgList struct {
	Lparen source.Pos
	Args   []*Arg
	Rparen source.Pos
}

func (l *ArgList) Pos() source.Pos { return l.Lparen }
func (l *ArgList) End() source.Pos { return l.Rparen + 1 }

// Arg is an argument: name: value. Name == nil is a positional argument
// (allowed only for built-in functions and conversions).
type Arg struct {
	Name  *Ident
	Colon source.Pos
	Value Expr
}

func (a *Arg) Pos() source.Pos {
	if a.Name != nil {
		return a.Name.Pos()
	}
	return a.Value.Pos()
}
func (a *Arg) End() source.Pos { return a.Value.End() }
