package sema

import (
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

var invalid types.Type = &types.Invalid{}

// Member names for the E0102 message.
var (
	memberField   = "field"
	memberIndex   = "index"
	memberID      = "line"
	memberValue   = "value"
	memberRequest = "request"
	methodKinds   = map[types.MethodKind]string{
		types.Command:      "command",
		types.Event:        "event",
		types.Notification: "notification",
		types.Error:        "error",
		types.Query:        "query",
		types.Rule:         "rule",
		types.ValueMethod:  "method",
		types.Request:      "request",
	}
)

// resolveID creates the identifier type of an entity, actor or projection.
// Base types are filled in after all identifiers are created: a projection
// may be based on the identifier of an entity declared below.
func (c *checker) resolveID(d *types.Decl) {
	if !d.Kind.HasID() {
		return
	}
	d.ID = &types.ID{Owner: d, Base: invalid}
}

// idBase fills in the base type of an identifier from the id line.
func (c *checker) idBase(d *types.Decl, members []ast.Member, name *ast.Ident) {
	var decl *ast.IDDecl
	for _, m := range members {
		id, ok := m.(*ast.IDDecl)
		if !ok {
			continue
		}
		if decl != nil {
			c.bag.Add(diag.E0102, rangeOf(id.Type), d.Name, memberID, "id").
				Note(rangeOf(decl.Type), noteDeclared)
			continue
		}
		decl = id
	}
	if decl == nil {
		c.bag.Add(diag.E0110, rangeOf(name), d.Name)
		return
	}
	base := c.idType(d, decl.Type)
	c.info.TypeExprs[decl.Type] = base
	d.ID.Base = base
}

var (
	textIDKinds   = "an id is declared as uuid, sequence, string or a value type"
	textIDDerived = "an id based on another one (id Order.Id) is only allowed in a projection"
	textIDOwner   = "a projection id can only be based on an entity or actor id"
)

func (c *checker) idType(d *types.Decl, t ast.TypeExpr) types.Type {
	nt, ok := t.(*ast.NamedType)
	if !ok {
		c.bag.Add(diag.E0111, rangeOf(t), textIDKinds)
		return invalid
	}
	if len(nt.Path) == 1 {
		switch name := nt.Path[0].Name; name {
		case "uuid":
			return types.Basics["uuid"]
		case "sequence":
			return types.Basics["bigint"]
		case "string":
			return types.Basics["string"]
		}
	}
	rt := c.resolveType(t)
	switch rt := rt.(type) {
	case *types.Invalid:
		return invalid
	case *types.Value:
		return rt
	case *types.ID:
		switch {
		case d.Kind != types.ProjectionDecl:
			c.bag.Add(diag.E0111, rangeOf(t), textIDDerived)
		case rt.Owner.Kind == types.ProjectionDecl:
			c.bag.Add(diag.E0111, rangeOf(t), textIDOwner)
		default:
			return rt
		}
		return invalid
	}
	c.bag.Add(diag.E0111, rangeOf(t), textIDKinds)
	return invalid
}

// collectMembers collects the members of a declaration and resolves their types.
func (c *checker) collectMembers(d *types.Decl) {
	switch n := d.Node.(type) {
	case *ast.EntityDecl:
		c.idBase(d, n.Members, n.Name)
		c.typeMembers(d, n.Members)
		c.innerMembers(d, n.Members)
		c.createCommands(d, n.Members)
	case *ast.ProjectionDecl:
		c.idBase(d, n.Members, n.Name)
		c.typeMembers(d, n.Members)
	case *ast.ValueDecl:
		c.typeMembers(d, n.Members)
	case *ast.ExternalDecl:
		for _, m := range n.Members {
			if c.prog.FromFake(m) {
				d.Fake = true
				d.ID = &types.ID{Owner: d, Base: types.Basics["uuid"]}
				break
			}
		}
		c.typeMembers(d, n.Members)
		c.fakeImpls(d, n.Members)
	case *ast.EnumDecl:
		for i, v := range n.Values {
			c.lowerName(v.Name, kindEnumValue)
			if prev := d.Value(v.Name.Name); prev != nil {
				c.bag.Add(diag.E0102, rangeOf(v.Name), d.Name, memberValue, v.Name.Name).
					Note(rangeOf(prev.Node.Name), noteDeclared)
				continue
			}
			d.AddValue(&types.EnumValue{Name: v.Name.Name, Node: v, Index: i})
		}
	case *ast.ScreenDecl:
		if n.Params != nil {
			d.Params = c.params(n.Params)
		}
	}
}

// typeMembers collects fields, indexes and methods. Namespaces: fields and
// indexes share one, methods another; a value object has fields and methods
// in one namespace, since both are read as x.name.
func (c *checker) typeMembers(d *types.Decl, members []ast.Member) {
	fields := map[string]*ast.Ident{}
	methods := map[string]*ast.Ident{}
	if d.Kind == types.ValueDecl {
		methods = fields
	}
	declare := func(ns map[string]*ast.Ident, name *ast.Ident, kind string) bool {
		if prev := ns[name.Name]; prev != nil {
			c.bag.Add(diag.E0102, rangeOf(name), d.Name, kind, name.Name).Note(rangeOf(prev), noteDeclared)
			return false
		}
		ns[name.Name] = name
		return true
	}
	for _, m := range members {
		c.info.Owner[m] = d
		switch m := m.(type) {
		case *ast.FieldDecl:
			c.lowerName(m.Name, kindField)
			c.builtinName(m.Name)
			if m.Personal != nil && c.prog.FromFake(m) {
				c.bag.Add(diag.E1209, rangeOf(m.Personal), fakePersonal)
			}
			t := c.resolveType(m.Type)
			if declare(fields, m.Name, memberField) {
				d.AddField(&types.Var{Name: m.Name.Name, Node: m, Type: t, Personal: m.Personal != nil})
			}
		case *ast.IndexDecl:
			c.lowerName(m.Name, kindIndex)
			if declare(fields, m.Name, memberIndex) {
				d.Indexes = append(d.Indexes, &types.Index{Name: m.Name.Name, Node: m})
			}
		case *ast.CommandDecl:
			c.method(d, declare(methods, m.Name, methodKinds[types.Command]), types.Command, m, m.Params, nil)
		case *ast.EventDecl:
			c.lowerName(m.Name, kindEvent)
			c.method(d, declare(methods, m.Name, methodKinds[types.Event]), types.Event, m, m.Params, nil)
		case *ast.NotificationDecl:
			c.lowerName(m.Name, kindNotif)
			c.method(d, declare(methods, m.Name, methodKinds[types.Notification]), types.Notification, m, m.Params, nil)
		case *ast.ErrorDecl:
			c.lowerName(m.Name, kindError)
			c.method(d, declare(methods, m.Name, methodKinds[types.Error]), types.Error, m, m.Params, nil)
		case *ast.QueryDecl:
			c.method(d, declare(methods, m.Name, methodKinds[types.Query]), types.Query, m, m.Params, m.Result)
		case *ast.RuleDecl:
			c.lowerName(m.Name, kindRule)
			c.method(d, declare(methods, m.Name, methodKinds[types.Rule]), types.Rule, m, m.Params, m.Result)
		case *ast.MethodDecl:
			c.lowerName(m.Name, kindMethod)
			c.method(d, declare(methods, m.Name, methodKinds[types.ValueMethod]), types.ValueMethod, m, m.Params, m.Result)
		case *ast.RequestDecl:
			if m.Body != nil {
				// An implementation in a fake: the body is checked with its own
				// parameters, the method is the declared request (fakeImpls).
				c.orphans[m] = c.newMethod(types.Request, m, m.Params, nil)
				continue
			}
			c.lowerName(m.Name, kindRequest)
			md := c.method(d, declare(methods, m.Name, memberRequest), types.Request, m, m.Params, nil)
			c.requestTimeout(d, m, md)
		}
	}
}

func (c *checker) method(d *types.Decl, add bool, kind types.MethodKind, m ast.Member, params *ast.ParamList, result ast.TypeExpr) *types.Method {
	md := c.newMethod(kind, m, params, result)
	if add {
		d.AddMethod(md)
	} else {
		// A repeat: there is no symbol in the type, but the body is still checked.
		c.orphans[m] = md
	}
	return md
}

// requestTimeout checks the deadline of a request: the timeout parameter is
// a model timer (Timer.Id or Timer.Id?); the old form "timeout 30s" is an error.
func (c *checker) requestTimeout(d *types.Decl, m *ast.RequestDecl, md *types.Method) {
	name := d.Name + "." + m.Name.Name
	declared := false
	for _, p := range md.Params {
		if p.Name != TimeoutParam {
			continue
		}
		declared = true
		if _, ok := types.Unwrap(p.Type).(*types.TimerID); !ok && !types.IsInvalid(p.Type) {
			c.bag.Add(diag.E1202, rangeOf(p.Node.(*ast.Param).Type), name, p.Type.String())
		}
	}
	if m.Timeout == nil {
		return
	}
	lit := "…"
	if bl, ok := m.Timeout.(*ast.BasicLit); ok {
		lit = bl.Raw
	}
	dg := c.bag.Add(diag.E1203, source.Range{Start: m.TimeoutPos, End: m.Timeout.End()}, lit)
	edits := []diag.Edit{{Range: source.Range{Start: m.Params.End(), End: m.Timeout.End()}}}
	if !declared {
		param := ", " + TimeoutParam + " Timer.Id"
		if len(m.Params.List) == 0 {
			param = TimeoutParam + " Timer.Id"
		}
		edits = append(edits, diag.Edit{Range: source.Range{Start: m.Params.Rparen, End: m.Params.Rparen}, NewText: param})
	}
	dg.Fix(diag.Message{Format: textTimerParam, Args: []any{lit}}, edits...)
}

var textTimerParam = "remove 'timeout %s' and declare a parameter timeout Timer.Id"

// fakeImpls links request implementations in a fake with the declared
// requests of the system: the same parameters, without timeout.
func (c *checker) fakeImpls(d *types.Decl, members []ast.Member) {
	seen := map[string]bool{}
	for _, m := range members {
		impl, ok := m.(*ast.RequestDecl)
		if !ok || impl.Body == nil {
			continue
		}
		decl := d.Method(impl.Name.Name)
		var want []string
		if decl != nil && decl.Kind == types.Request {
			for _, p := range decl.Params {
				if p.Name != TimeoutParam {
					want = append(want, p.Name+" "+p.Type.String())
				}
			}
		}
		var got []string
		for _, p := range c.orphans[impl].Params {
			got = append(got, p.Name+" "+p.Type.String())
		}
		header := source.Range{Start: impl.Request, End: impl.Params.End()}
		switch {
		case decl == nil || decl.Kind != types.Request:
			c.bag.Add(diag.E1207, header, impl.Name.Name+"(…)").
				Hint("system %s has no request %q", d.Name, impl.Name.Name)
		case seen[impl.Name.Name]:
			c.bag.Add(diag.E0102, rangeOf(impl.Name), d.Name, memberRequest, impl.Name.Name)
		case strings.Join(got, ", ") != strings.Join(want, ", "):
			c.bag.Add(diag.E1207, header, impl.Name.Name+"("+strings.Join(want, ", ")+")")
		default:
			c.info.Impls[impl] = decl
		}
		seen[impl.Name.Name] = true
	}
}

// TimeoutParam is the name of the parameter of an external system request
// that sets its deadline with a model timer (RFC §18).
const TimeoutParam = "timeout"

func (c *checker) newMethod(kind types.MethodKind, m ast.Member, params *ast.ParamList, result ast.TypeExpr) *types.Method {
	md := &types.Method{Kind: kind, Name: memberName(m), Node: m, Result: &types.Void{}}
	if params != nil {
		md.Params = c.params(params)
	}
	if result != nil {
		md.Result = c.resolveType(result)
	}
	return md
}

// methodOf returns the method symbol of a member declaration.
func (c *checker) methodOf(td *types.Decl, m ast.Member) *types.Method {
	if md := td.Method(memberName(m)); md != nil && md.Node == m {
		return md
	}
	return c.orphans[m]
}

func (c *checker) params(l *ast.ParamList) []*types.Var {
	var out []*types.Var
	for _, p := range l.List {
		c.lowerName(p.Name, kindParam)
		out = append(out, &types.Var{Name: p.Name.Name, Node: p, Type: c.resolveType(p.Type), Personal: p.Personal != nil})
	}
	return out
}

func memberName(m ast.Member) string {
	switch m := m.(type) {
	case *ast.CommandDecl:
		return m.Name.Name
	case *ast.EventDecl:
		return m.Name.Name
	case *ast.NotificationDecl:
		return m.Name.Name
	case *ast.ErrorDecl:
		return m.Name.Name
	case *ast.QueryDecl:
		return m.Name.Name
	case *ast.RuleDecl:
		return m.Name.Name
	case *ast.MethodDecl:
		return m.Name.Name
	case *ast.RequestDecl:
		return m.Name.Name
	}
	return ""
}
