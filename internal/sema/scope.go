package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
	"sort"
)

// Bounded contexts (docs/plans/0011-contexts-acl-aggregates.md, §3.6): a
// bounded context is a one-level namespace and the owner of its model. A
// declaration of bounded context "Ordering" is named by full name
// "Ordering.Money", "Ordering.Order"; roles are shared by the project.
// Another bounded context is visible only in its ACL (acl Ordering in an
// *.acl.rr file), in tests and outside bounded contexts, in apps and
// primitives. Apps and primitives are outside namespaces, as is everything
// in a flat module (bounded context "").

// ctxFile is a program file and its bounded context.
type ctxFile struct {
	src  *source.File
	ctx  string
	test bool // *.test.rr: a test calls the whole project (RFC §23.1)
	demo bool // *.demo.rr: a demo scenario, given only
	perf bool // *.perf.rr: a performance test, a path in when, expressions in then
}

// initScope collects the bounded contexts and ACLs of the program.
func (c *checker) initScope() {
	c.contexts = map[string]bool{}
	c.reported = map[source.Pos]bool{}
	for _, f := range c.prog.Files {
		c.ctxFiles = append(c.ctxFiles, ctxFile{src: f.Src, ctx: f.Context, test: f.Test, demo: f.Demo, perf: f.Perf})
		if f.Context != "" {
			c.contexts[f.Context] = true
		}
	}
	sort.Slice(c.ctxFiles, func(i, j int) bool { return c.ctxFiles[i].src.Base() < c.ctxFiles[j].src.Base() })
	for _, d := range c.prog.Decls {
		if a, ok := d.(*ast.ACLDecl); ok {
			c.aclDecls = append(c.aclDecls, a)
		}
	}
}

// ctxOf returns the bounded context of the file that holds a node.
func (c *checker) ctxOf(n ast.Node) string {
	if n == nil {
		return ""
	}
	if f := c.fileOf(n.Pos()); f != nil {
		return f.ctx
	}
	return ""
}

// fileOf returns the program file that holds position pos, or nil.
func (c *checker) fileOf(pos source.Pos) *ctxFile {
	i := sort.Search(len(c.ctxFiles), func(i int) bool { return c.ctxFiles[i].src.Base() > int(pos) }) - 1
	if i >= 0 && c.ctxFiles[i].src.Contains(pos) {
		return &c.ctxFiles[i]
	}
	return nil
}

// qualify returns the full name of declaration short in bounded context ctx.
func qualify(ctx, short string) string {
	if ctx == "" {
		return short
	}
	return ctx + "." + short
}

// aclAt returns the source of the ACL that holds a node, or "".
func (c *checker) aclAt(n ast.Node) string {
	pos := n.Pos()
	for _, a := range c.aclDecls {
		if a.Pos() <= pos && pos < a.End() && a.Source != nil {
			return a.Source.Name
		}
	}
	return ""
}

// lookup returns the declaration that path path names from place at (the
// whole path is the declaration name), or nil.
func (c *checker) lookup(at ast.Node, path ...string) *types.Decl {
	d, n := c.lookupPrefix(at, path)
	if n != len(path) {
		return nil
	}
	return d
}

// lookupPrefix is like find, but a declaration of another bounded context
// where it is not visible is error E1407 (once per place).
func (c *checker) lookupPrefix(at ast.Node, path []string) (*types.Decl, int) {
	d, n := c.find(at, path)
	if d != nil {
		c.checkSeen(at, d)
	}
	return d, n
}

// find finds a declaration by the start of path path from place at and
// returns it and the number of path parts used; the remaining parts are
// members (Id, a method, an enum value). A path starts with a bounded
// context (Billing.Money) or a short name: it is looked up in its own
// bounded context, then outside namespaces. Outside bounded contexts (apps,
// primitives) and in tests a short name is looked up in the whole project
// and must be unique.
func (c *checker) find(at ast.Node, path []string) (*types.Decl, int) {
	if len(path) == 0 {
		return nil, 0
	}
	ctx := c.ctxOf(at)
	// Inside an aggregate the short name of its inner entity is that entity.
	if agg := c.aggOf(at); agg != nil {
		if in := agg.InnerNamed(path[0]); in != nil {
			return in, 1
		}
	}
	// A bounded context at the start of a path wins over a same-named app:
	// types cannot have such names (E1406).
	if len(path) > 1 && c.contexts[path[0]] {
		if d := c.short(path[0], path[1]); d != nil {
			return innerPath(d, path, 2)
		}
	} else if d := c.short(ctx, path[0]); d != nil {
		return innerPath(d, path, 1)
	}
	if (ctx == "" || c.inTest(at)) && len(c.contexts) > 0 {
		if d := c.unique(path[0]); d != nil {
			return innerPath(d, path, 1)
		}
	}
	return nil, 0
}

// innerPath continues the path from aggregate d to its inner entity:
// Order.Stop; n is the number of path parts that already name d.
func innerPath(d *types.Decl, path []string, n int) (*types.Decl, int) {
	if n < len(path) {
		if in := d.InnerNamed(path[n]); in != nil {
			return in, n + 1
		}
	}
	return d, n
}

// sees reports whether declaration d is visible from place at: from its own
// bounded context or outside contexts, always; from another context,
// outside contexts, in tests and in the ACL of that context: entities and
// actors (identifiers, events, public commands), value objects and enums.
func (c *checker) sees(at ast.Node, d *types.Decl) bool {
	ctx := c.ctxOf(at)
	if d.Context == "" || d.Context == ctx || ctx == "" || c.inTest(at) {
		return true
	}
	if c.aclAt(at) != d.Context {
		return false
	}
	switch d.Kind {
	case types.EntityDecl, types.ActorDecl, types.ValueDecl, types.EnumDecl:
		return true
	}
	return false
}

// checkSeen reports E1407 if declaration d is not visible from place at.
func (c *checker) checkSeen(at ast.Node, d *types.Decl) bool {
	if c.sees(at, d) {
		return true
	}
	c.closed(at, d.Name, d.Context)
	return false
}

// closed reports E1407 about member name of bounded context ctx, once per place.
func (c *checker) closed(at ast.Node, name, ctx string) {
	if !c.reported[at.Pos()] {
		c.reported[at.Pos()] = true
		c.bag.Add(diag.E1407, rangeOf(at), name, ctx)
	}
}

// isNamespace reports whether a name names a bounded context.
func (c *checker) isNamespace(_ ast.Node, name string) bool { return c.contexts[name] }

// inTest reports whether a node stands in a test file.
func (c *checker) inTest(n ast.Node) bool {
	if f := c.fileOf(n.Pos()); f != nil {
		return f.test
	}
	return false
}

// short looks up a short name from bounded context ctx: a declaration of
// the context, then a declaration outside namespaces.
func (c *checker) short(ctx, name string) *types.Decl {
	if ctx != "" {
		if d := c.info.Decls[ctx+"."+name]; d != nil {
			return d
		}
	}
	return c.info.Decls[name]
}

// unique looks up a short name in the whole project: there must be one
// declaration with that name.
func (c *checker) unique(name string) *types.Decl {
	return c.info.uniqueShort(name)
}

// path returns the parts of x.y.z made of identifiers, or nil if it is not
// a path (a call, an index, ?.).
func path(x ast.Expr) []*ast.Ident {
	switch x := x.(type) {
	case *ast.Ident:
		return []*ast.Ident{x}
	case *ast.SelectorExpr:
		if x.Safe {
			return nil
		}
		if p := path(x.X); p != nil {
			return append(p, x.Sel)
		}
	}
	return nil
}

func names(ids []*ast.Ident) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.Name
	}
	return out
}

// visible reports whether declaration d is fully visible from place at: in
// its own bounded context and outside namespaces. Another bounded context
// (in its ACL, in tests) sees only public commands and queries, events,
// value types and identifiers.
func (c *checker) visible(d *types.Decl, at ast.Node) bool {
	return d.Context == "" || d.Context == c.ctxOf(at)
}

// entityMethod looks up method name of declaration d from place at. hidden:
// the method exists but is not visible from here (a private method of
// another bounded context).
func (c *checker) entityMethod(d *types.Decl, name string, at ast.Node) (owner *types.Decl, m *types.Method, hidden *types.Decl) {
	m = d.Method(name)
	if m == nil || d.Kind != types.EntityDecl && d.Kind != types.ActorDecl {
		return d, m, nil
	}
	ctx := c.ctxOf(at)
	if m.Public() || ctx == "" || d.Context == ctx {
		return d, m, nil
	}
	return d, nil, d
}

// checkNamespaces checks the names in namespaces: a type is not named like
// a bounded context (E1406), external systems have unique names.
func (c *checker) checkNamespaces() {
	externals := map[string]*types.Decl{}
	for _, d := range c.info.DeclList {
		if d.Context == "" || d.Kind > types.ExternalDecl {
			continue
		}
		name := declIdent(d.Node)
		if d.Kind == types.ExternalDecl {
			// An integration's external system is known by its short name.
			if prev := externals[d.Short]; prev != nil {
				c.bag.Add(diag.E0101, rangeOf(name), d.Short,
					"external systems of a project have unique names: integrations know them by the short name").
					Note(rangeOf(declIdent(prev.Node)), noteDeclared)
			}
			externals[d.Short] = d
		}
		if c.contexts[d.Short] {
			c.bag.Add(diag.E1406, rangeOf(name), d.Short)
		}
	}
}

// role returns a project role by name.
func (c *checker) role(_ ast.Node, name string) *ast.RoleDecl {
	return c.info.Roles[name]
}

// Lookup returns a declaration by name for tools (explain, graph, LSP): the
// full name or the short name if it is unique in the model.
func (i *Info) Lookup(name string) *types.Decl {
	if d := i.Decls[name]; d != nil {
		return d
	}
	return i.uniqueShort(name)
}

// onEvent returns the event of an on reaction: its own (on placed) or, in
// an ACL, an event of an entity of the source bounded context (on
// Ordering.Order.placed).
func (c *checker) onEvent(owner *types.Decl, on *ast.OnDecl) *types.Method {
	if len(on.Path) == 0 {
		if ev := owner.Method(on.Event.Name); ev != nil && ev.Kind == types.Event {
			return ev
		}
		return nil
	}
	if t, ok := c.info.On[on]; ok {
		if t == nil {
			return nil
		}
		return t.Event
	}
	c.info.On[on] = nil
	where := names2(on.Path) + "." + on.Event.Name
	src := c.aclAt(on)
	if src == "" {
		c.bag.Add(diag.E1415, source.Range{Start: on.Path[0].Pos(), End: on.Event.End()}, where, on.Path[0].Name)
		return nil
	}
	d, n := c.find(on.Path[0], names(on.Path))
	if d == nil || n != len(on.Path) || d.Context != src || d.Kind != types.EntityDecl && d.Kind != types.ActorDecl || len(on.Path) != 2 {
		c.bag.Add(diag.E1416, source.Range{Start: on.Path[0].Pos(), End: on.Event.End()}, where, src)
		return nil
	}
	ev := d.Method(on.Event.Name)
	if ev == nil || ev.Kind != types.Event {
		c.bag.Add(diag.E1006, rangeOf(on.Event), d.Name, on.Event.Name)
		return nil
	}
	c.info.On[on] = &OnEvent{Model: d, Event: ev}
	return ev
}

// Resolve finds a declaration by the start of a path from place at, as in
// the model (lookup by namespace): the declaration and the number of parts used.
func (i *Info) Resolve(at ast.Node, path []string) (*types.Decl, int) {
	if i.scope == nil {
		return nil, 0
	}
	return i.scope.find(at, path)
}

// Namespace returns the bounded context ("Ordering") that path path names
// from place at; "" when the path does not name a bounded context.
func (i *Info) Namespace(_ ast.Node, path []string) string {
	c := i.scope
	if c == nil || len(path) != 1 || !c.contexts[path[0]] {
		return ""
	}
	return path[0]
}
