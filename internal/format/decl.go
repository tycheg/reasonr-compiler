package format

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

func (p *printer) decl(d ast.Decl) doc {
	switch d := d.(type) {
	case *ast.RoleDecl:
		return cat(text("role "), p.ident(d.Name))
	case *ast.FeatureDecl:
		return cat(text("feature "), p.ident(d.Name), text(" "), p.lit(d.Date))
	case *ast.EnumDecl:
		return cat(text("enum "), p.ident(d.Name), p.trailing(d.Name.End()),
			body(p, d.Values, func(v *ast.EnumValue) doc {
				if v.Was != nil {
					return cat(p.ident(v.Name), text(" was "), p.ident(v.Was))
				}
				return p.ident(v.Name)
			}))
	case *ast.ValueDecl:
		return p.typeDecl("value ", d.Name, d.Members)
	case *ast.EntityDecl:
		switch {
		case d.IsActor:
			return p.typeDecl("actor ", d.Name, d.Members)
		case d.IsAggregate:
			return p.typeDecl("aggregate ", d.Name, d.Members)
		case d.Owner != nil:
			return cat(text("entity "), p.ident(d.Owner), text("."), p.ident(d.Name), p.trailing(d.Name.End()), members(p, d.Members, p.member))
		}
		return p.typeDecl("entity ", d.Name, d.Members)
	case *ast.ProjectionDecl:
		return p.typeDecl("projection ", d.Name, d.Members)
	case *ast.ExternalDecl:
		return cat(text("external"), p.access(d.Access), text(" "), p.ident(d.Name), p.trailing(d.Name.End()), members(p, d.Members, p.member))
	case *ast.FakeDecl:
		return p.typeDecl("fake ", d.Name, d.Members)
	case *ast.ACLDecl:
		return p.typeDecl("acl ", d.Source, d.Members)
	case *ast.AppDecl:
		return p.app(d)
	case *ast.ScreenDecl:
		return p.screen(d)
	case *ast.DefaultDecl:
		return p.defaultDecl(d)
	case *ast.PrimitiveDecl:
		return p.primitive(d)
	case *ast.TextsDecl:
		return p.texts(d)
	case *ast.MigrationDecl:
		return cat(text("migration "), p.ident(d.Name), p.trailing(d.Name.End()), p.stmts(d.Body))
	case *ast.TestDecl:
		return p.test(d)
	case *ast.ExtendDecl:
		if d.IsScreen() {
			return cat(text("extend screen "), p.ident(d.Name), p.trailing(d.Name.End()), body(p, d.Ins, func(in *ast.ExtendIn) doc {
				return cat(text("in "), p.ident(in.Part), p.trailing(in.Part.End()), body(p, in.Nodes, p.node))
			}))
		}
		return p.typeDecl("extend ", d.Name, d.Members)
	case *ast.ExcludeDecl:
		return cat(text("exclude test "), p.lit(d.Test), text(" because "), p.lit(d.Reason))
	case *ast.LetDecl:
		return p.stmt(d.Let)
	}
	panic("format: unexpected declaration")
}

func (p *printer) typeDecl(kw string, name *ast.Ident, ms []ast.Member) doc {
	return cat(text(kw), p.ident(name), p.trailing(name.End()), members(p, ms, p.member))
}

func (p *printer) member(m ast.Member) doc {
	switch m := m.(type) {
	case *ast.IDDecl:
		return cat(text("id "), p.typ(m.Type))
	case *ast.FieldDecl:
		d := cat(p.ident(m.Name), text(" "), p.typ(m.Type))
		if m.Default != nil {
			d = cat(d, text(" = "), p.expr(m.Default))
		}
		if m.Personal != nil {
			d = cat(d, text(" "), p.personal(m.Personal))
		}
		if m.Was != nil {
			d = cat(d, text(" was "), p.ident(m.Was))
		}
		return d
	case *ast.IndexDecl:
		d := cat(text("index "), p.ident(m.Name),
			listNoComma(p, "(", m.Lparen, m.Keys, m.Rparen, ")", p.sortKey))
		if m.Error != nil {
			d = cat(d, text(" unique "), p.ident(m.Error))
		}
		return d
	case *ast.CommandDecl:
		head := cat(text("command"), p.access(m.Access), text(" "))
		if m.Create.IsValid() {
			head = cat(head, text("create "))
		}
		head = cat(head, p.ident(m.Name), p.params(m.Params))
		end := m.Params.End()
		if m.Was != nil {
			head, end = cat(head, text(" was "), p.ident(m.Was)), m.Was.End()
		}
		return cat(head, p.trailing(end), p.stmts(m.Body))
	case *ast.EventDecl:
		d := cat(text("event "), p.ident(m.Name), p.params(m.Params))
		if m.Body != nil {
			d = cat(d, p.trailing(m.Params.End()), p.stmts(m.Body))
		}
		return d
	case *ast.NotificationDecl:
		return cat(text("notification "), p.ident(m.Name), p.params(m.Params))
	case *ast.ErrorDecl:
		d := cat(text("error "), p.ident(m.Name))
		if m.Params != nil {
			d = cat(d, p.params(m.Params))
		}
		return d
	case *ast.QueryDecl:
		head := cat(text("query"), p.access(m.Access), text(" "), p.ident(m.Name), p.params(m.Params),
			text(" "), p.typ(m.Result))
		end := m.Result.End()
		if m.Was != nil {
			head, end = cat(head, text(" was "), p.ident(m.Was)), m.Was.End()
		}
		return cat(head, p.trailing(end), p.stmts(m.Body))
	case *ast.RuleDecl:
		head := cat(text("rule "), p.ident(m.Name), p.params(m.Params), text(" "), p.typ(m.Result))
		return cat(head, p.trailing(m.Result.End()), p.stmts(m.Body))
	case *ast.OnDecl:
		var head doc = text("on ")
		for _, id := range m.Path {
			head = cat(head, p.ident(id), text("."))
		}
		return cat(head, p.ident(m.Event), p.trailing(m.Event.End()), p.stmts(m.Body))
	case *ast.ActorAsDecl:
		var head doc = text("actor ")
		for i, id := range m.From {
			if i > 0 {
				head = cat(head, text("."))
			}
			head = cat(head, p.ident(id))
		}
		return cat(head, text(" as "), p.ident(m.To))
	case *ast.CheckDecl:
		return cat(text("check "), p.expr(m.Cond))
	case *ast.MethodDecl:
		head := cat(p.ident(m.Name), p.params(m.Params), text(" "), p.typ(m.Result))
		return cat(head, p.trailing(m.Result.End()), p.stmts(m.Body))
	case *ast.RequestDecl:
		d := cat(text("request "), p.ident(m.Name), p.params(m.Params))
		if m.Timeout != nil {
			d = cat(d, text(" timeout "), p.expr(m.Timeout))
		}
		if m.Body != nil {
			end := m.Params.End()
			if m.Timeout != nil {
				end = m.Timeout.End()
			}
			d = cat(d, p.trailing(end), p.stmts(m.Body))
		}
		return d
	}
	panic("format: unexpected member")
}

func (p *printer) access(a *ast.AccessList) doc {
	if a == nil {
		return nil
	}
	return list(p, "(", a.Lparen, a.Names, a.Rparen, ")", p.ident)
}

func (p *printer) params(l *ast.ParamList) doc {
	return list(p, "(", l.Lparen, l.List, l.Rparen, ")", p.param)
}

func (p *printer) param(prm *ast.Param) doc {
	d := cat(p.ident(prm.Name), text(" "), p.typ(prm.Type))
	if prm.Default != nil {
		d = cat(d, text(" = "), p.expr(prm.Default))
	}
	if prm.Personal != nil {
		d = cat(d, text(" "), p.personal(prm.Personal))
	}
	if prm.Was != nil {
		d = cat(d, text(" was "), p.ident(prm.Was))
	}
	return d
}

func (p *printer) personal(pers *ast.Personal) doc {
	if pers.Subject == nil {
		return text("personal")
	}
	return listNoComma(p, "personal(", pers.PersonalPos, []*ast.Path{pers.Subject}, pers.Rparen, ")",
		func(path *ast.Path) doc { return text(path.String()) })
}

func (p *printer) sortKey(k *ast.SortKey) doc {
	if k.Desc() {
		return text(k.Path.String() + " desc")
	}
	return text(k.Path.String())
}

// ---------------------------------------------------------------------------
// Tests

func (p *printer) test(d *ast.TestDecl) doc {
	var items []ast.Node
	if d.Features != nil {
		items = append(items, d.Features)
	}
	for _, s := range d.Setup {
		items = append(items, s)
	}
	if d.Given != nil {
		items = append(items, d.Given)
	}
	if d.When != nil {
		items = append(items, d.When)
	}
	if d.Then != nil {
		items = append(items, d.Then)
	}
	return cat(text("test "), p.lit(d.Name), p.trailing(d.Name.End()), body(p, items, p.testItem))
}

func (p *printer) testItem(n ast.Node) doc {
	switch n := n.(type) {
	case *ast.GivenBlock:
		return p.section("given", n.Given, n.Steps)
	case *ast.WhenBlock:
		return p.section("when", n.When, n.Steps)
	case *ast.ThenBlock:
		return cat(text("then"), p.trailing(n.Then+source.Pos(len("then"))), body(p, n.Items, p.thenItem))
	case *ast.TestFeatures:
		var d doc = text("feature ")
		for i, name := range n.Names {
			if i > 0 {
				d = cat(d, text(", "))
			}
			d = cat(d, p.ident(name))
		}
		return d
	case ast.Stmt:
		return p.stmt(n)
	}
	panic("format: unexpected test part")
}

func (p *printer) section(word string, pos source.Pos, steps []ast.TestStep) doc {
	return cat(text(word), p.trailing(pos+source.Pos(len(word))), body(p, steps, p.testStep))
}

func (p *printer) testStep(s ast.TestStep) doc {
	switch s := s.(type) {
	case *ast.TestCall:
		return p.testCall(s)
	case *ast.TimeOffset:
		return p.timeOffset(s)
	case *ast.TestStmt:
		return p.stmt(s.Stmt)
	}
	panic("format: unexpected test step")
}

// timeOffset prints time offset with a sign: the old time passes d becomes
// time offset +d (RFC §27.5).
func (p *printer) timeOffset(s *ast.TimeOffset) doc {
	if u, ok := s.Duration.(*ast.UnaryExpr); ok && u.Op == token.SUB {
		return cat(text("time offset "), p.expr(s.Duration))
	}
	return cat(text("time offset +"), p.expr(s.Duration))
}

func (p *printer) testCall(c *ast.TestCall) doc {
	d := p.expr(c.Call)
	if c.As != nil {
		d = cat(d, text(" as "), p.ident(c.As))
	}
	return d
}

func (p *printer) thenItem(t ast.ThenItem) doc {
	switch t := t.(type) {
	case *ast.ThenExpr:
		return p.expr(t.X)
	case *ast.ThenNotification:
		d := cat(text("notification "), p.ident(t.Name))
		if t.Args != nil {
			d = cat(d, p.args(t.Args))
		}
		if t.To != nil {
			d = cat(d, text(" to "), p.expr(t.To))
		}
		return d
	case *ast.ThenError:
		d := cat(text("error "), p.ident(t.Name))
		if t.Args != nil {
			d = cat(d, p.args(t.Args))
		}
		return d
	case *ast.ThenRequest:
		d := cat(text("request "), p.ident(t.System), text("."), p.ident(t.Name))
		if t.Args != nil {
			d = cat(d, p.args(t.Args))
		}
		return d
	case *ast.ThenMatches:
		d := cat(text("matches "), p.args(t.Fields))
		if t.Call != nil {
			d = cat(p.testCall(t.Call), text(" "), d)
		}
		return d
	}
	panic("format: unexpected test check")
}
