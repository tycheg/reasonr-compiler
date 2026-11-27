package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Aggregates (docs/plans/0011-contexts-acl-aggregates.md, §3.5–3.8):
// aggregate Order is a consistency boundary, the root; entity Order.Stop is
// an inner entity with its own identity inside the aggregate. Inner
// entities are stored in the aggregate document, have no public commands
// and queries, no indexes, and are addressed only inside their aggregate.

// topEntity reports E1419: there is no top-level entity, only aggregate.
func (c *checker) topEntity(e *ast.EntityDecl) {
	kw := source.Range{Start: e.Keyword, End: e.Keyword + source.Pos(len("entity"))}
	c.bag.Add(diag.E1419, source.Range{Start: e.Keyword, End: e.Name.End()}, e.Name.Name).
		Fix(diag.Message{Format: "replace entity with aggregate"},
			diag.Edit{Range: kw, NewText: "aggregate"})
}

// linkInner links inner entities with their aggregates (E1420).
func (c *checker) linkInner(inner []*types.Decl) {
	for _, td := range inner {
		e := td.Node.(*ast.EntityDecl)
		agg := c.info.Decls[qualify(td.Context, e.Owner.Name)]
		if agg == nil || agg.Owner != nil || agg.Kind != types.EntityDecl && agg.Kind != types.ActorDecl {
			c.bag.Add(diag.E1420, rangeOf(e.Owner), e.Owner.Name, e.Name.Name)
			continue
		}
		td.Owner = agg
		agg.Inner = append(agg.Inner, td)
	}
}

// innerMembers checks the members of an inner entity: it has no public
// commands and queries, no queries and no indexes (E1421).
func (c *checker) innerMembers(d *types.Decl, members []ast.Member) {
	if d.Owner == nil {
		return
	}
	for _, m := range members {
		var name *ast.Ident
		switch m := m.(type) {
		case *ast.CommandDecl:
			if types.IsUpper(m.Name.Name) {
				name = m.Name
			}
		case *ast.QueryDecl:
			name = m.Name
		case *ast.IndexDecl:
			name = m.Name
		}
		if name != nil {
			c.bag.Add(diag.E1421, rangeOf(name), name.Name, d.Owner.Short+"."+d.Short)
		}
	}
}

// innerAt checks that inner entity d is addressed from inside its aggregate
// (E1422): outside, its identifier means nothing. Tests call the whole project.
func (b *body) innerAt(n ast.Node, d *types.Decl) bool {
	if d.Owner == nil || b.kind == ctxTest || b.c.aggOf(n) == d.Owner {
		return true
	}
	b.c.bag.Add(diag.E1422, rangeOf(n), d.Owner.Short+"."+d.Short, d.Owner.Short)
	return false
}

// checkInnerIDs checks §3.7: the identifier of an inner entity (directly or
// in a value object) is stored and passed only by its aggregate and its
// inner entities: fields, parameters, results (E1423).
func (c *checker) checkInnerIDs() {
	for _, d := range c.info.DeclList {
		switch d.Kind {
		case types.EntityDecl, types.ActorDecl, types.ProjectionDecl, types.ExternalDecl:
		default:
			continue
		}
		root := d.Root()
		check := func(t types.Type, at ast.Node) {
			if in := innerIn(t, map[*types.Decl]bool{}); in != nil && in.Owner != root {
				c.bag.Add(diag.E1423, rangeOf(at), in.Owner.Short+"."+in.Short, in.Owner.Short)
			}
		}
		for _, f := range d.Fields {
			if fd, ok := f.Node.(*ast.FieldDecl); ok {
				check(f.Type, fd.Type)
			}
		}
		for _, m := range d.Methods {
			for _, p := range m.Params {
				if pn, ok := p.Node.(*ast.Param); ok {
					check(p.Type, pn.Type)
				}
			}
			if q, ok := m.Node.(*ast.QueryDecl); ok && q.Result != nil {
				check(m.Result, q.Result)
			}
		}
	}
}

// innerIn returns the inner entity whose identifier occurs in type t (value
// object fields included), or nil.
func innerIn(t types.Type, seen map[*types.Decl]bool) *types.Decl {
	switch t := types.Unwrap(t).(type) {
	case *types.ID:
		if t.Owner.Owner != nil {
			return t.Owner
		}
	case *types.Value:
		if seen[t.Decl] {
			return nil
		}
		seen[t.Decl] = true
		for _, f := range t.Decl.Fields {
			if in := innerIn(f.Type, seen); in != nil {
				return in
			}
		}
	case *types.List:
		return innerIn(t.Elem, seen)
	case *types.Set:
		return innerIn(t.Elem, seen)
	case *types.Map:
		if in := innerIn(t.Key, seen); in != nil {
			return in
		}
		return innerIn(t.Elem, seen)
	case *types.Page:
		return innerIn(t.Elem, seen)
	}
	return nil
}

// innerHere checks [stop] and [Stop]: the records of an inner entity are
// read by commands, rules and reactions of its aggregate, which have an
// aggregate instance.
func (b *body) innerHere(x ast.Node, d *types.Decl) bool {
	if !b.innerAt(x, d) {
		return false
	}
	switch b.kind {
	case ctxCommand, ctxRule, ctxOn, ctxTest:
		return true
	}
	b.c.bag.Add(diag.E1422, rangeOf(x), d.Owner.Short+"."+d.Short, d.Owner.Short)
	return false
}

// createCommands checks creation commands (§3.8): a public command of an
// aggregate whose identifier the engine issues, uuid or sequence (E1424).
func (c *checker) createCommands(d *types.Decl, members []ast.Member) {
	for _, m := range members {
		cmd, ok := m.(*ast.CommandDecl)
		if !ok || !cmd.Create.IsValid() {
			continue
		}
		var why string
		switch {
		case d.Owner != nil:
			why = "an inner entity has no create command: an aggregate command creates it"
		case !types.IsUpper(cmd.Name.Name):
			why = "a create command is public and capitalized"
		case d.ID == nil || !isBasic(d.ID.Base, types.UUID) && !isBasic(d.ID.Base, types.Bigint):
			why = "the engine issues the id: id uuid or id sequence"
		default:
			continue
		}
		c.bag.Add(diag.E1424, source.Range{Start: cmd.Create, End: cmd.Name.End()}, cmd.Name.Name, why)
	}
}
