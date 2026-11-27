package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// ctxKind is the kind of body the code is checked in (plan, §11.6).
type ctxKind uint8

const (
	ctxCommand ctxKind = iota
	ctxEvent
	ctxQuery
	ctxRule
	ctxMethod // a value object method
	ctxCheck  // a check invariant of a value object
	ctxOn     // an extension reaction
	ctxMigration
	ctxTest    // shared test lets and lets in a test
	ctxDefault // the default value of a field or parameter
	ctxUI      // layout expressions: binding arguments, if conditions
)

// ctxNames name the body kinds for messages: "in a command".
var ctxNames = [...]string{
	ctxCommand:   "a command",
	ctxEvent:     "an event",
	ctxQuery:     "a query",
	ctxRule:      "a rule",
	ctxMethod:    "a value method",
	ctxCheck:     "a check invariant",
	ctxOn:        "an on reaction",
	ctxMigration: "a migration",
	ctxTest:      "a test",
	ctxDefault:   "a default value",
	ctxUI:        "a layout",
}

// body is the state of checking one body.
type body struct {
	c      *checker
	kind   ctxKind
	owner  *types.Decl   // the type that owns the body; nil for migrations and tests
	method *types.Method // the checked method or nil
	result types.Type    // the result type for return (queries, rules, methods)
	me     types.Type    // the type of me, or nil if me is not available

	scopes []map[string]*Object
	// record holds the fields of the search record: in where, sort and select bare names are its fields.
	record *types.Decl

	// flag is the flag of the condition whose branch is being checked
	// (E1502); flagReads are the flags read since the start of the current
	// condition (E1503).
	flag      *ast.Ident
	flagReads []*ast.Ident

	// acl: an ACL reaction; the source entity fields are not visible, there
	// is no after, id is the source entity identifier.
	acl bool

	facts   facts // narrowed types
	errored bool  // an error is raised on the current path (for queries, E0507)
	loops   int   // loop nesting depth
	perf    bool  // a then check of a performance test: stats and baseline are visible
}

func (c *checker) newBody(kind ctxKind, owner *types.Decl, m *types.Method) *body {
	b := &body{c: c, kind: kind, owner: owner, method: m, scopes: []map[string]*Object{{}}, facts: facts{}}
	if m != nil {
		b.result = m.Result
		for _, p := range m.Params {
			b.declare(&Object{Kind: ObjParam, Name: p.Name, Type: p.Type, Node: p.Node, Var: p})
		}
	}
	return b
}

func (b *body) push() { b.scopes = append(b.scopes, map[string]*Object{}) }
func (b *body) pop()  { b.scopes = b.scopes[:len(b.scopes)-1] }

func (b *body) declare(o *Object) { b.scopes[len(b.scopes)-1][o.Name] = o }

// fake reports that the body belongs to an external system fake: the model
// is visible to it only through the API (RFC §18).
func (b *body) fake() bool { return b.owner != nil && b.owner.Kind == types.ExternalDecl }

// notInFake reports E1209 if the body belongs to a fake.
func (b *body) notInFake(n ast.Node, what string) bool {
	if !b.fake() {
		return true
	}
	b.c.bag.Add(diag.E1209, rangeOf(n), what)
	return false
}

// What is not available in a fake, for E1209.
var (
	fakeRef      = "access to a model entity or projection"
	fakeSafe     = "?. (it checks whether the entity exists)"
	fakeQuery    = "a model query"
	fakeRequest  = "an external request"
	fakeUpsert   = "a projection update"
	fakeForget   = "forget"
	fakePersonal = "personal (a fake stores test data)"
	fakeID       = "id (a fake has a single instance)"
)

// fieldsVisible reports whether the owner's fields are read by bare name.
func (b *body) fieldsVisible() bool {
	switch b.kind {
	case ctxCommand, ctxEvent, ctxRule, ctxMethod, ctxCheck, ctxOn:
		return b.owner != nil && !b.acl
	}
	return false
}

// lookup looks up a name: the search record, variables and parameters,
// fields, built-in values. The caller looks up enum values and types.
func (b *body) lookup(id *ast.Ident) *Object {
	name := id.Name
	if b.record != nil {
		if o := recordField(b.record, name); o != nil {
			return o
		}
	}
	for i := len(b.scopes) - 1; i >= 0; i-- {
		if o := b.scopes[i][name]; o != nil {
			return o
		}
	}
	if b.fieldsVisible() {
		if f := b.owner.Field(name); f != nil {
			return &Object{Kind: ObjField, Name: name, Type: f.Type, Node: f.Node, Var: f}
		}
	}
	if o := b.c.fixtures[name]; o != nil && b.kind == ctxTest {
		return o
	}
	if o := b.builtin(id); o != nil {
		return o
	}
	if f := b.c.feature(id, name); f != nil {
		return b.featureRead(id, f)
	}
	return nil
}

// recordField returns a record field (id included) or nil.
func recordField(d *types.Decl, name string) *Object {
	if name == "id" && d.ID != nil {
		return &Object{Kind: ObjField, Name: name, Type: d.ID}
	}
	if f := d.Field(name); f != nil {
		return &Object{Kind: ObjField, Name: name, Type: f.Type, Node: f.Node, Var: f}
	}
	return nil
}

// Why a built-in value is unavailable, by body kind.
var (
	whyReplay = "events must be replayable"
	whyPure   = "it is a pure function"
	whyQuery  = "a query belongs to the type, not to an instance"
	whyHere   = "the value is not defined here"
	whyMe     = "me is only available in methods with (<Actor>) access"
)

// builtin returns the built-in value if the name is built-in. If it is not
// available in this body kind, E0304 is reported and an Invalid object is returned.
func (b *body) builtin(id *ast.Ident) *Object {
	var t types.Type
	allowed := false
	switch id.Name {
	case "id":
		if b.fake() {
			b.c.bag.Add(diag.E1209, rangeOf(id), fakeID)
			return &Object{Kind: ObjBuiltin, Name: id.Name, Type: invalid}
		}
		if b.owner != nil && b.owner.ID != nil {
			t = b.owner.ID
		}
		allowed = t != nil && (b.kind == ctxCommand || b.kind == ctxEvent || b.kind == ctxOn || b.kind == ctxRule)
	case "now":
		t = tTimestamp
		allowed = b.kind == ctxCommand || b.kind == ctxQuery || b.kind == ctxOn || b.kind == ctxMigration || b.kind == ctxTest
	case "today":
		t = tDate
		allowed = b.kind == ctxCommand || b.kind == ctxQuery || b.kind == ctxOn || b.kind == ctxMigration || b.kind == ctxTest
	case "after":
		if b.owner != nil {
			t = &types.Record{Owner: b.owner}
		}
		allowed = t != nil && (b.kind == ctxCommand || b.kind == ctxOn) && !b.acl
	case "hasErrors":
		t = tBool
		allowed = b.kind == ctxCommand || b.kind == ctxQuery || b.kind == ctxOn
	case "me":
		t = b.me
		allowed = t != nil
	case "caller":
		t = &types.Caller{}
		allowed = b.kind == ctxCommand || b.kind == ctxQuery
	case "stats", "baseline":
		if !b.perf {
			return nil
		}
		t, allowed = perfStatsType, true
	default:
		return nil
	}
	if !allowed {
		b.c.bag.Add(diag.E0304, rangeOf(id), ctxNames[b.kind], id.Name, b.why(id.Name))
		return &Object{Kind: ObjBuiltin, Name: id.Name, Type: invalid}
	}
	return &Object{Kind: ObjBuiltin, Name: id.Name, Type: t}
}

func (b *body) why(name string) string {
	switch {
	case name == "me":
		return whyMe
	case b.kind == ctxEvent:
		return whyReplay
	case b.kind == ctxRule || b.kind == ctxMethod || b.kind == ctxCheck || b.kind == ctxDefault:
		return whyPure
	case b.kind == ctxQuery && (name == "id" || name == "after"):
		return whyQuery
	}
	return whyHere
}

// ---------------------------------------------------------------------------
// Entry points

// checkBodies checks all bodies of the model: methods, reactions,
// migrations, default values, invariants and shared test lets.
func (c *checker) checkBodies(decls []ast.Decl) {
	c.fixtures = map[string]*Object{}
	for _, d := range decls {
		if l, ok := d.(*ast.LetDecl); ok {
			b := c.newBody(ctxTest, nil, nil)
			t := b.let(l.Let)
			if _, dup := c.fixtures[l.Let.Name.Name]; !dup {
				c.fixtures[l.Let.Name.Name] = &Object{Kind: ObjFixture, Name: l.Let.Name.Name, Type: t, Node: l.Let.Name}
			}
		}
	}
	// Bodies and invariants first: default values of value objects are
	// computed with the already checked checks.
	for _, td := range c.info.DeclList {
		switch {
		case td.Kind == types.EntityDecl, td.Kind == types.ActorDecl, td.Kind == types.ProjectionDecl, td.Kind == types.ValueDecl:
			c.memberBodies(td)
		case td.Fake:
			c.memberBodies(td)
		}
	}
	for _, td := range c.info.DeclList {
		c.checkDefaults(td)
	}
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.MigrationDecl:
			b := c.newBody(ctxMigration, nil, nil)
			b.fnBody(d.Body, d.Name)
		case *ast.TestDecl:
			c.checkTest(d)
		}
	}
}

func (c *checker) memberBodies(td *types.Decl) {
	var members []ast.Member
	switch n := td.Node.(type) {
	case *ast.EntityDecl:
		members = n.Members
	case *ast.ProjectionDecl:
		members = n.Members
	case *ast.ValueDecl:
		members = n.Members
	case *ast.ExternalDecl:
		members = n.Members
	}
	for _, m := range members {
		switch m := m.(type) {
		case *ast.RequestDecl:
			if m.Body != nil {
				b := c.newBody(ctxCommand, td, c.orphans[m])
				b.fnBody(m.Body, m.Name)
			}
		case *ast.CommandDecl:
			b := c.newBody(ctxCommand, td, c.methodOf(td, m))
			b.me = c.meType(m.Access)
			b.fnBody(m.Body, m.Name)
		case *ast.EventDecl:
			if m.Body != nil {
				b := c.newBody(ctxEvent, td, c.methodOf(td, m))
				b.fnBody(m.Body, m.Name)
			}
		case *ast.QueryDecl:
			b := c.newBody(ctxQuery, td, c.methodOf(td, m))
			b.me = c.meType(m.Access)
			b.fnBody(m.Body, m.Name)
		case *ast.RuleDecl:
			b := c.newBody(ctxRule, td, c.methodOf(td, m))
			b.fnBody(m.Body, m.Name)
		case *ast.MethodDecl:
			b := c.newBody(ctxMethod, td, c.methodOf(td, m))
			b.fnBody(m.Body, m.Name)
		case *ast.OnDecl:
			b := c.newBody(ctxOn, td, nil)
			if ev := c.onEvent(td, m); ev != nil {
				for _, p := range ev.Params {
					b.declare(&Object{Kind: ObjParam, Name: p.Name, Type: p.Type, Node: p.Node, Var: p})
				}
			}
			b.fnBody(m.Body, m.Event)
		case *ast.CheckDecl:
			b := c.newBody(ctxCheck, td, nil)
			b.cond(m.Cond)
		}
	}
}

// meType returns the type of me by the access list: the identifier of the
// only actor in the list, or nil.
func (c *checker) meType(l *ast.AccessList) types.Type {
	if l == nil {
		return nil
	}
	var actor *types.Decl
	for _, id := range l.Names {
		if a, ok := c.info.Access[id]; ok && a.Kind == AccessActor {
			if actor != nil && actor != a.Actor {
				return nil
			}
			actor = a.Actor
		}
	}
	if actor == nil {
		return nil
	}
	return actor.ID
}

// fnBody checks a body and its execution paths (plan, §11.8): a query
// returns a value or raises an error on every path, a rule and a method
// return a value.
func (b *body) fnBody(blk *ast.Block, name *ast.Ident) {
	if blk == nil || b.block(blk) {
		return
	}
	switch b.kind {
	case ctxQuery:
		b.c.bag.Add(diag.E0507, rangeOf(name))
	case ctxRule:
		b.c.bag.Add(diag.E0508, rangeOf(name), "a rule")
	case ctxMethod:
		b.c.bag.Add(diag.E0508, rangeOf(name), "a method")
	}
}
