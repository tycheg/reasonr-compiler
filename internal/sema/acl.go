package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Anti-corruption layer (docs/plans/0011-contexts-acl-aggregates.md,
// §3.1–3.3): acl Ordering in bounded context Dispatch translates the
// Ordering model into the Dispatch model. Of the source, the ACL sees
// entities and actors (identifiers, events, public commands), value objects
// and enums; of its own bounded context, everything. A reaction on
// Ordering.Order.placed runs in the same unit of work after the root
// command, in event order; id in it is the identifier of the source entity,
// the event parameters are variables. actor Auth.User as Customer: a
// calling actor of the source acts as an actor of its own bounded context
// with the same identifier.

// ACL is the anti-corruption layer of a bounded context.
type ACL struct {
	Context string // its own bounded context
	Source  string // the source bounded context
	Node    *ast.ACLDecl
}

// checkACLs checks ACL declarations: the source, reactions, actors.
func (c *checker) checkACLs() {
	seen := map[string]*ast.ACLDecl{}
	for _, a := range c.aclDecls {
		if a.Source == nil {
			continue
		}
		ctx := c.ctxOf(a)
		src := a.Source.Name
		if ctx == "" || !c.contexts[src] || src == ctx {
			c.bag.Add(diag.E1414, rangeOf(a.Source), src)
			continue
		}
		key := ctx + "\x00" + src
		if prev := seen[key]; prev != nil {
			c.bag.Add(diag.E0101, rangeOf(a.Source), src, "a context has one ACL per source").
				Note(rangeOf(prev.Source), noteDeclared)
			continue
		}
		seen[key] = a
		c.info.ACLs = append(c.info.ACLs, &ACL{Context: ctx, Source: src, Node: a})
		for _, m := range a.Members {
			switch m := m.(type) {
			case *ast.OnDecl:
				c.aclOn(a, m)
			case *ast.ActorAsDecl:
				c.actorAs(ctx, src, m)
			}
		}
	}
}

// aclOn checks an ACL reaction to a source event.
func (c *checker) aclOn(a *ast.ACLDecl, on *ast.OnDecl) {
	if len(on.Path) == 0 {
		c.bag.Add(diag.E1416, rangeOf(on.Event), on.Event.Name, a.Source.Name)
		return
	}
	ev := c.onEvent(nil, on)
	if ev == nil {
		return
	}
	owner := c.info.On[on].Model
	s := c.newScope(nil)
	for _, p := range ev.Node.(*ast.EventDecl).Params.List {
		s.blocks[0][p.Name.Name] = p.Name
	}
	s.block(on.Body)
	b := c.newBody(ctxOn, owner, nil)
	b.acl = true
	for _, p := range ev.Params {
		b.declare(&Object{Kind: ObjParam, Name: p.Name, Type: p.Type, Node: p.Node, Var: p})
	}
	b.fnBody(on.Body, on.Event)
}

// actorAs checks actor Source.Actor as Actor: on the left an actor of the
// source, on the right an actor of its own bounded context with the same
// kind of identifier.
func (c *checker) actorAs(ctx, src string, m *ast.ActorAsDecl) {
	if m.To == nil || len(m.From) == 0 {
		return
	}
	from, n := c.find(m.From[0], names(m.From))
	to := c.info.Decls[qualify(ctx, m.To.Name)]
	at := source.Range{Start: m.From[0].Pos(), End: m.To.End()}
	if from == nil || n != len(m.From) || from.Kind != types.ActorDecl || from.Context != src ||
		to == nil || to.Kind != types.ActorDecl {
		c.bag.Add(diag.E1417, at, names2(m.From), m.To.Name, src)
		return
	}
	if from.ID == nil || to.ID == nil || !identical(from.ID.Base, to.ID.Base) {
		c.bag.Add(diag.E1417, at, names2(m.From), m.To.Name, src)
		return
	}
	for _, x := range to.Callers {
		if x == from {
			c.bag.Add(diag.E0101, at, from.Name, "a source actor acts as one actor")
			return
		}
	}
	to.Callers = append(to.Callers, from)
}

// translatesID reports whether X.Id(y) translates an identifier of an entity
// of another bounded context src into an identifier of d with the same base
// type (docs/plans/0011, §3.2, option A): allowed in an ACL, in tests and
// outside bounded contexts.
func (b *body) translatesID(at ast.Node, src *types.ID, d *types.Decl) bool {
	from := src.Owner
	if from.Context == d.Context || !entityKind(from) || !entityKind(d) || !identical(src.Base, d.ID.Base) {
		return false
	}
	c := b.c
	return c.ctxOf(at) == "" || c.inTest(at) || c.aclAt(at) != ""
}

func entityKind(d *types.Decl) bool { return d.Kind == types.EntityDecl || d.Kind == types.ActorDecl }

// enumConvert is Enum(x): an enum value of another bounded context is
// translated by name (in an ACL, in tests and outside bounded contexts).
// The target enum has all values of the source one (E1418). nil means the
// expression is not a translation, and the caller reports an error.
func (b *body) enumConvert(x *ast.CallExpr, fun ast.Node, d *types.Decl) types.Type {
	c := b.c
	if len(x.Args.Args) != 1 || x.Args.Args[0].Name != nil {
		return nil
	}
	if ctx := c.ctxOf(x); ctx != "" && !c.inTest(x) && c.aclAt(x) == "" {
		return nil
	}
	arg := x.Args.Args[0].Value
	t := b.expr(arg, nil)
	if types.IsInvalid(t) {
		return &types.Enum{Decl: d}
	}
	if b.optionalOperand(arg, t) {
		return &types.Enum{Decl: d}
	}
	src, ok := t.(*types.Enum)
	if !ok || src.Decl == d {
		b.c.bag.Add(diag.E0214, rangeOf(fun), diag.Message{Format: textNotCallable, Args: []any{d.Short}})
		return &types.Enum{Decl: d}
	}
	for _, v := range src.Decl.Values {
		if d.Value(v.Name) == nil {
			c.bag.Add(diag.E1418, rangeOf(arg), d.Name, src.Decl.Name, v.Name)
			return &types.Enum{Decl: d}
		}
	}
	c.info.Calls[x] = &Call{Kind: CallEnumConvert, Decl: d}
	return &types.Enum{Decl: d}
}
