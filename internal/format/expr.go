package format

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

func (p *printer) ident(x *ast.Ident) doc { return text(x.Name) }

func (p *printer) lit(x *ast.BasicLit) doc { return text(x.Raw) }

// raw returns the source text [from, to) unchanged.
func (p *printer) raw(from, to source.Pos) doc {
	return text(p.src.Source()[p.src.Offset(from):p.src.Offset(to)])
}

func (p *printer) expr(x ast.Expr) doc {
	switch x := x.(type) {
	case *ast.Ident:
		return p.ident(x)
	case *ast.BasicLit:
		return p.lit(x)
	case *ast.InterpString:
		// The string prints as is: escapes and expressions inside braces stay
		// as the author wrote them.
		return p.raw(x.Pos(), x.End())
	case *ast.ParenExpr:
		return listNoComma(p, "(", x.Lparen, []ast.Expr{x.X}, x.Rparen, ")", p.expr)
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			return cat(text("not "), p.expr(x.X))
		}
		return cat(text("-"), p.expr(x.X))
	case *ast.BinaryExpr:
		return cat(p.expr(x.X), text(" "+x.Op.String()+" "), p.expr(x.Y))
	case *ast.IsNoneExpr:
		if x.Not {
			return cat(p.expr(x.X), text(" is not none"))
		}
		return cat(p.expr(x.X), text(" is none"))
	case *ast.InExpr:
		if x.Not {
			return cat(p.expr(x.X), text(" not in "), p.expr(x.Y))
		}
		return cat(p.expr(x.X), text(" in "), p.expr(x.Y))
	case *ast.SelectorExpr:
		dot := "."
		if x.Safe {
			dot = "?."
		}
		return cat(p.expr(x.X), text(dot), p.ident(x.Sel))
	case *ast.CallExpr:
		return cat(p.expr(x.Fun), p.args(x.Args))
	case *ast.BracketExpr:
		return list(p, "[", x.Lbrack, x.Elems, x.Rbrack, "]", p.expr)
	case *ast.MapLit:
		if len(x.Entries) == 0 {
			if p.hasComments(x.Lbrack, x.Rbrack) || p.line(x.Lbrack) != p.line(x.Rbrack) && p.docAfter(x.Rbrack) {
				return cat(text("["), p.trailing(x.Lbrack+1), p.listComments(x.Rbrack), hardline, text(":]"))
			}
			return text("[:]")
		}
		return list(p, "[", x.Lbrack, x.Entries, x.Rbrack, "]", func(e *ast.MapEntry) doc {
			return cat(p.expr(e.Key), text(": "), p.expr(e.Value))
		})
	case *ast.SearchExpr:
		return p.search(x)
	case *ast.NewIDExpr:
		if x.Type == nil {
			return text("new")
		}
		return text("new " + x.Type.String())
	case *ast.AllExpr:
		return cat(text("all "), p.ident(x.Type))
	}
	panic("format: unexpected expression")
}

func (p *printer) args(l *ast.ArgList) doc {
	return list(p, "(", l.Lparen, l.Args, l.Rparen, ")", func(a *ast.Arg) doc {
		if a.Name == nil {
			return p.expr(a.Value)
		}
		return cat(p.ident(a.Name), text(": "), p.expr(a.Value))
	})
}

// list prints elements in brackets. If the list does not fit on a line or
// has comments inside, every element goes on its own line with a trailing
// comma, and the closing bracket goes on a separate line.
//
// A "##" documentation comment at the end of a line belongs to the
// declaration starting on that line, so the formatter does not move it
// between lines: a comment on the line of the opening bracket stays there,
// and a list with "##" on the line of the closing bracket stays broken.
func list[T ast.Node](p *printer, open string, lpos source.Pos, items []T, rpos source.Pos, close string, f func(T) doc) doc {
	return listOf(p, true, open, lpos, items, rpos, close, f)
}

// listNoComma is like list but without a comma after the last element where
// the grammar does not allow one: index keys, the target of a view action.
func listNoComma[T ast.Node](p *printer, open string, lpos source.Pos, items []T, rpos source.Pos, close string, f func(T) doc) doc {
	return listOf(p, false, open, lpos, items, rpos, close, f)
}

func listOf[T ast.Node](p *printer, comma bool, open string, lpos source.Pos, items []T, rpos source.Pos, close string, f func(T) doc) doc {
	sep := func(i int) doc {
		if comma || i < len(items)-1 {
			return text(",")
		}
		return nil
	}
	if p.hasComments(lpos, rpos) || p.line(lpos) != p.line(rpos) && p.docAfter(rpos) {
		first := p.trailing(lpos + 1)
		l := &lines{p: p}
		for i, it := range items {
			l.add(it.Pos(), it.End(), func() doc { return cat(f(it), sep(i)) })
		}
		l.comments(rpos)
		return cat(text(open), first, l.listDoc(), hardline, text(close))
	}
	if len(items) == 0 {
		return text(open + close)
	}
	var parts concat
	for i, it := range items {
		if i > 0 {
			parts = append(parts, text(","), spaceline)
		}
		parts = append(parts, f(it))
	}
	var last doc
	if comma {
		last = &ifBreak{brk: text(",")}
	}
	return grp(text(open), indent(softline, parts, last), softline, text(close))
}

// listDoc is the indented lines of a broken list, without blank lines.
func (l *lines) listDoc() doc {
	if len(l.out) == 0 {
		return nil
	}
	for i := range l.out {
		l.out[i].blank = false
	}
	return indent(hardline, l.doc())
}

// listComments is the comments up to position to as lines of a broken list.
func (p *printer) listComments(to source.Pos) doc {
	l := &lines{p: p}
	l.comments(to)
	return l.listDoc()
}

// docAfter reports whether "##" follows pos on the same line.
func (p *printer) docAfter(pos source.Pos) bool {
	i := p.firstFrom(pos)
	return i < len(p.comments) && !p.used[i] && p.comments[i].Doc && p.line(p.comments[i].Pos) == p.line(pos)
}

// search prints a search: [Type] and sections on the next lines.
func (p *printer) search(s *ast.SearchExpr) doc {
	type clause struct {
		pos, end source.Pos
		d        func() doc
	}
	var cs []clause
	if s.Where != nil {
		cs = append(cs, clause{s.WherePos, s.Where.End(), func() doc { return cat(text("where "), p.expr(s.Where)) }})
	}
	if len(s.Sort) > 0 {
		cs = append(cs, clause{s.SortPos, s.Sort[len(s.Sort)-1].End(), func() doc {
			var parts concat
			for i, k := range s.Sort {
				if i > 0 {
					parts = append(parts, text(", "))
				}
				parts = append(parts, p.sortKey(k))
			}
			return cat(text("sort "), parts)
		}})
	}
	if s.Page != nil {
		cs = append(cs, clause{s.PagePos, s.Page.End(), func() doc { return cat(text("page "), p.expr(s.Page)) }})
	}
	if s.Limit != nil {
		cs = append(cs, clause{s.LimitPos, s.Limit.End(), func() doc { return cat(text("limit "), p.expr(s.Limit)) }})
	}
	if s.Select != nil {
		cs = append(cs, clause{s.SelectPos, s.Select.End(), func() doc { return cat(text("select "), p.expr(s.Select)) }})
	}
	head := cat(listNoComma(p, "[", s.Lbrack, []*ast.Ident{s.Type}, s.Rbrack, "]", p.ident), p.trailing(s.Rbrack+1))
	l := &lines{p: p}
	for _, c := range cs {
		l.add(c.pos, c.end, c.d)
	}
	if len(cs) > 0 {
		l.tail(p.col(cs[0].pos), cs[len(cs)-1].end)
	}
	return cat(head, indent(hardline, l.doc()))
}

func (p *printer) typ(t ast.TypeExpr) doc { return text(ast.TypeString(t)) }
