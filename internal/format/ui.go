package format

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// UI: apps, screens, layout, primitives, default, texts.

func (p *printer) app(d *ast.AppDecl) doc {
	head := cat(text("app"), p.access(d.Access), text(" "), p.ident(d.Name))
	end := d.Name.End()
	if d.Title != nil {
		head = cat(head, text(" "), p.lit(d.Title))
		end = d.Title.End()
	}
	if d.IsAuto() {
		head = cat(head, text(" auto"))
		end = d.AutoPos + source.Pos(len("auto"))
	}
	if len(d.Items) == 0 {
		return head
	}
	return cat(head, p.trailing(end), body(p, d.Items, p.appItem))
}

func (p *printer) appItem(it ast.AppItem) doc {
	switch it := it.(type) {
	case *ast.LanguageItem:
		return cat(text("language "), p.ident(it.Name))
	case *ast.PlatformsItem:
		var d concat
		d = append(d, text("platforms "))
		for i, id := range it.Names {
			if i > 0 {
				d = append(d, text(", "))
			}
			d = append(d, p.ident(id))
		}
		return d
	case *ast.DefaultDecl:
		return p.defaultDecl(it)
	case ast.UINode:
		return p.node(it)
	}
	panic("format: unexpected app line")
}

func (p *printer) defaultDecl(d *ast.DefaultDecl) doc {
	out := cat(text("default "), p.typ(d.Type), text(" "), p.ident(d.Prim))
	if d.Args != nil {
		out = cat(out, p.args(d.Args))
	}
	return out
}

func (p *printer) screen(d *ast.ScreenDecl) doc {
	head := cat(text("screen "), p.ident(d.Name))
	end := d.Name.End()
	if d.Params != nil {
		head = cat(head, p.params(d.Params))
		end = d.Params.End()
	}
	if d.Title != nil {
		head = cat(head, text(" "), p.lit(d.Title))
		end = d.Title.End()
	}
	return cat(head, p.trailing(end), body(p, d.Nodes, p.node))
}

// node prints a layout line and its children.
func (p *printer) node(n ast.UINode) doc {
	switch n := n.(type) {
	case *ast.PrimNode:
		d := p.ident(n.Name)
		end := n.Name.End()
		if n.Args != nil {
			d = cat(d, p.args(n.Args))
			end = n.Args.End()
		}
		if n.Binding != nil {
			d = cat(d, text(" "), p.binding(n.Binding))
			end = n.Binding.End()
		}
		d, end = p.nodeTail(d, end, n.Label, n.Cond)
		return p.nodeChildren(d, end, n.Children)
	case *ast.FieldNode:
		d := p.ident(n.Name)
		end := n.Name.End()
		if n.Prim != nil {
			d = cat(d, text(" "), p.ident(n.Prim))
			end = n.Prim.End()
			if n.PrimArgs != nil {
				d = cat(d, p.args(n.PrimArgs))
				end = n.PrimArgs.End()
			}
		}
		d, end = p.nodeTail(d, end, n.Label, n.Cond)
		return p.nodeChildren(d, end, n.Children)
	case *ast.OnNode:
		d := cat(text("on "), p.ident(n.Event))
		if n.IsBack() {
			return cat(d, text(" back"))
		}
		d = cat(d, text(" "), p.ident(n.Screen))
		if n.Args != nil {
			d = cat(d, p.args(n.Args))
		}
		return d
	}
	panic("format: unexpected layout line")
}

func (p *printer) binding(b *ast.Binding) doc {
	var d doc
	if b.Target != nil {
		d = cat(listNoComma(p, "[", b.Target.Lbrack, b.Target.Elems, b.Target.Rbrack, "]", p.expr), text("."), p.ident(b.Method))
	} else {
		d = p.ident(b.Type)
		if b.Method != nil {
			d = cat(d, text("."), p.ident(b.Method))
		}
	}
	if b.Args != nil {
		d = cat(d, p.args(b.Args))
	}
	return d
}

func (p *printer) nodeTail(d doc, end source.Pos, label *ast.BasicLit, cond ast.Expr) (doc, source.Pos) {
	if label != nil {
		d = cat(d, text(" "), p.lit(label))
		end = label.End()
	}
	if cond != nil {
		d = cat(d, text(" if "), p.expr(cond))
		end = cond.End()
	}
	return d, end
}

func (p *printer) nodeChildren(d doc, end source.Pos, children []ast.UINode) doc {
	if len(children) == 0 {
		return d
	}
	return cat(d, p.trailing(end), body(p, children, p.node))
}

// ---------------------------------------------------------------------------
// Primitives

func (p *printer) primitive(d *ast.PrimitiveDecl) doc {
	head := cat(text("primitive "), p.ident(d.Name))
	end := d.Name.End()
	if l := d.TypeParams; l != nil {
		head = cat(head, list(p, "<", l.Lt, l.List, l.Gt, ">", func(tp *ast.TypeParam) doc {
			if tp.Constraint == nil {
				return p.ident(tp.Name)
			}
			return cat(p.ident(tp.Name), text(" "), p.ident(tp.Constraint))
		}))
		end = l.End()
	}
	if l := d.Params; l != nil {
		head = cat(head, list(p, "(", l.Lparen, l.List, l.Rparen, ")", p.primParam))
		end = l.End()
	}
	if len(d.Items) == 0 {
		return head
	}
	return cat(head, p.trailing(end), body(p, d.Items, p.primItem))
}

func (p *printer) primParam(pp *ast.PrimParam) doc {
	d := p.ident(pp.Name)
	if pp.Query != nil {
		d = cat(d, p.params(pp.Query))
	}
	d = cat(d, text(" "), p.typ(pp.Type))
	switch {
	case pp.IsAuto():
		d = cat(d, text(" = auto"))
	case pp.Default != nil:
		d = cat(d, text(" = "), p.expr(pp.Default))
	}
	return d
}

func (p *printer) primItem(it ast.PrimItem) doc {
	switch it := it.(type) {
	case *ast.BindsItem:
		var d doc = text("binds")
		if it.Kind != "" {
			d = cat(d, text(" "+it.Kind))
		}
		if it.Type != nil {
			d = cat(d, text(" "), p.typ(it.Type))
		}
		return d
	case *ast.PartItem:
		var d doc = text("children")
		if !it.Children {
			d = cat(text("part "), p.ident(it.Name))
		}
		if it.BindsPos.IsValid() {
			d = cat(d, text(" binds"))
			if it.Kind != "" {
				d = cat(d, text(" "+it.Kind))
			}
			if it.Type != nil {
				d = cat(d, text(" "), p.typ(it.Type))
			}
		}
		return d
	case *ast.PrimEvent:
		d := cat(text("event "), p.ident(it.Name))
		if it.Params != nil {
			d = cat(d, p.params(it.Params))
		}
		return d
	}
	panic("format: unexpected primitive line")
}

// ---------------------------------------------------------------------------
// Texts

func (p *printer) texts(d *ast.TextsDecl) doc {
	return cat(text("texts "), p.ident(d.Lang), p.trailing(d.Lang.End()), body(p, d.Entries, func(e *ast.TextEntry) doc {
		return cat(text(e.Key.String()+" "), p.expr(e.Value))
	}))
}
