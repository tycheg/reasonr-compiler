package ast

import (
	"strconv"

	"github.com/tycheg/reasonr-compiler/internal/source"
)

// TextsDecl is the UI texts in a language (RFC §22.11): texts en and lines
// `key "text"`.
type TextsDecl struct {
	Doc     *CommentGroup
	Texts   source.Pos
	Lang    *Ident
	Entries []*TextEntry
	EndPos  source.Pos
}

// TextEntry is a key and a text; Value is a *BasicLit or an *InterpString
// with {parameter} placeholders.
type TextEntry struct {
	Key   *TextKey
	Value Expr
}

// TextKey is a text key: a path to a model name (Order.cancelNotAllowed) or
// a layout node Screen:binding[#n] (AdminOrder:Order.Edit, AdminOrders:Table#2).
type TextKey struct {
	Path  *Path
	Colon source.Pos // NoPos for a model name
	Node  *Path      // or nil
	Hash  source.Pos // NoPos for the first node
	N     *BasicLit  // node number or nil
}

// IsNode reports whether the key refers to a layout node.
func (k *TextKey) IsNode() bool { return k.Node != nil }

// Number returns the node number: 1 when it is not given.
func (k *TextKey) Number() int {
	if k.N == nil {
		return 1
	}
	n, err := strconv.Atoi(k.N.Raw)
	if err != nil {
		return 0
	}
	return n
}

func (k *TextKey) String() string {
	s := k.Path.String()
	if k.Node != nil {
		s += ":" + k.Node.String()
	}
	if k.N != nil {
		s += "#" + k.N.Raw
	}
	return s
}

func (d *TextsDecl) Pos() source.Pos { return d.Texts }
func (d *TextsDecl) End() source.Pos { return d.EndPos }
func (e *TextEntry) Pos() source.Pos { return e.Key.Pos() }
func (e *TextEntry) End() source.Pos { return e.Value.End() }
func (k *TextKey) Pos() source.Pos   { return k.Path.Pos() }
func (k *TextKey) End() source.Pos {
	switch {
	case k.N != nil:
		return k.N.End()
	case k.Node != nil:
		return k.Node.End()
	}
	return k.Path.End()
}

func (*TextsDecl) declNode() {}
