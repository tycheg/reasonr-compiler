package sema

import (
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Actions for E0303 "<action> is not allowed in <body kind>".
var (
	actCommand  = "call commands"
	actEvent    = "raise events"
	actNotify   = "send notifications"
	actError    = "raise errors"
	actRule     = "call rules"
	actQuery    = "call queries"
	actRequest  = "send external requests"
	actUpsert   = "update projections"
	actSchedule = "schedule timers"
	actMethod   = "call value methods without a receiver"
)

var (
	textNotCallable  = "%q cannot be called"
	textNoCommand    = "command %q"
	textNoMethod     = "method %q"
	textNoQuery      = "query %q"
	textRefTarget    = "[x].command(…): x must be an entity or actor id"
	textVoidValue    = "%s returns nothing: write the call as a separate statement"
	textUpsertExpr   = "updating projection %s is a statement; write it on a separate line"
	textConvertArity = "%s.Id(…) takes one value"
)

// call checks a call; stmt: the call stands as a separate statement.
func (b *body) call(x *ast.CallExpr, want types.Type, stmt bool) types.Type {
	t := b.call0(x, want, stmt)
	b.setType(x, t)
	c := b.c.info.Calls[x]
	if c == nil {
		return t
	}
	switch {
	case stmt && c.Kind == CallUpsert:
	case stmt && !isVoid(t) && !types.IsInvalid(t) && c.Builtin != "schedule":
		b.c.bag.Add(diag.E0213, rangeOf(x), b.c.quote(x.Fun))
	case !stmt && c.Kind == CallUpsert:
		b.c.bag.Add(diag.E0214, rangeOf(x), diag.Message{Format: textUpsertExpr, Args: []any{c.Decl.Name}})
		return invalid
	case !stmt && isVoid(t):
		b.c.bag.Add(diag.E0214, rangeOf(x), diag.Message{Format: textVoidValue, Args: []any{b.c.quote(x.Fun)}})
		return invalid
	}
	return t
}

func isVoid(t types.Type) bool {
	_, ok := t.(*types.Void)
	return ok
}

func (b *body) call0(x *ast.CallExpr, want types.Type, stmt bool) types.Type {
	switch fun := x.Fun.(type) {
	case *ast.Ident:
		return b.callName(x, fun, stmt)
	case *ast.SelectorExpr:
		if br, ok := fun.X.(*ast.BracketExpr); ok {
			return b.commandCall(x, fun, br)
		}
		if b.isFaker(fun) {
			return b.fakerCall(x, fun)
		}
		if id, ok := fun.X.(*ast.Ident); ok && b.perf && fun.Sel.Name == "of" && (id.Name == "stats" || id.Name == "baseline") && b.lookupQuiet(id.Name) == nil {
			b.expr(fun.X, nil)
			return b.perfOf(x)
		}
		if id, ok := fun.X.(*ast.Ident); ok && !b.c.isNamespace(id, id.Name) {
			if d := b.isTypeRef(id); d != nil {
				return b.typeCall(x, fun, d)
			}
		}
		// A full name: Billing.Money(...), Ordering.Order.Card(...), Auth.User.Id(x).
		if p := path(fun); len(p) > 1 && b.lookupQuiet(p[0].Name) == nil && !b.fieldNamed(p[0].Name) {
			if d, n := b.c.lookupPrefix(fun, names(p)); d != nil {
				switch n {
				case len(p):
					return b.construct(x, fun, d, d.Short, stmt)
				case len(p) - 1:
					return b.typeCall(x, fun, d)
				}
			}
		}
		return b.methodCall(x, fun)
	}
	b.expr(x.Fun, nil)
	b.skipArgs(x.Args)
	b.c.bag.Add(diag.E0214, rangeOf(x.Fun), diag.Message{Format: textNotCallable, Args: []any{b.c.quote(x.Fun)}})
	return invalid
}

// skipArgs checks the arguments without matching them to parameters (after an error).
func (b *body) skipArgs(l *ast.ArgList) {
	for _, a := range l.Args {
		b.expr(a.Value, nil)
	}
}

// callName is name(...): a method of its own type, a constructor, an upsert, a built-in function.
func (b *body) callName(x *ast.CallExpr, fun *ast.Ident, stmt bool) types.Type {
	name := fun.Name
	if b.owner != nil {
		if m := b.owner.Method(name); m != nil && m.Kind != types.Query && m.Kind != types.Request {
			b.c.info.Calls[x] = &Call{Kind: CallMethod, Decl: b.owner, Method: m}
			b.allowMethod(x, m)
			b.args(x, quoteName(name), m.Params)
			return m.Result
		}
	}
	if d := b.c.lookup(fun, name); d != nil {
		return b.construct(x, fun, d, name, stmt)
	}
	if bf := builtinFuncs[name]; bf != nil {
		b.c.info.Calls[x] = &Call{Kind: CallBuiltin, Builtin: name}
		return bf(b, x)
	}
	if name == "range" {
		return b.rangeCall(x)
	}
	b.skipArgs(x.Args)
	b.unknown(fun)
	return invalid
}

// construct is Type(...): a value object constructor or a projection row
// (only its own bounded context writes a projection).
func (b *body) construct(x *ast.CallExpr, fun ast.Node, d *types.Decl, name string, stmt bool) types.Type {
	switch d.Kind {
	case types.ValueDecl:
		b.c.info.Calls[x] = &Call{Kind: CallConstruct, Decl: d}
		b.args(x, quoteName(name), d.Fields)
		return &types.Value{Decl: d}
	case types.EnumDecl:
		if t := b.enumConvert(x, fun, d); t != nil {
			return t
		}
	case types.ProjectionDecl:
		if !b.c.visible(d, fun) {
			b.skipArgs(x.Args)
			b.c.closed(fun, name, d.Context)
			return invalid
		}
		b.c.info.Calls[x] = &Call{Kind: CallUpsert, Decl: d}
		if stmt {
			b.allow(x, actUpsert, ctxCommand, ctxOn)
			b.notInFake(x, fakeUpsert)
		}
		params := append([]*types.Var{{Name: "id", Type: d.ID}}, d.Fields...)
		b.upsertArgs(x, name, params)
		return tVoid
	}
	b.skipArgs(x.Args)
	b.c.bag.Add(diag.E0214, rangeOf(fun), diag.Message{Format: textNotCallable, Args: []any{name}})
	return invalid
}

func quoteName(name string) string { return "'" + name + "'" }

// allowMethod checks whether a method of its own type may be called in this body.
func (b *body) allowMethod(x *ast.CallExpr, m *types.Method) {
	switch m.Kind {
	case types.Command:
		b.allow(x, actCommand, ctxCommand, ctxOn)
	case types.Event:
		b.allow(x, actEvent, ctxCommand, ctxOn)
	case types.Notification:
		b.allow(x, actNotify, ctxCommand, ctxOn)
	case types.Error:
		b.allow(x, actError, ctxCommand, ctxQuery, ctxOn)
	case types.Rule:
		b.allow(x, actRule, ctxCommand, ctxQuery, ctxRule, ctxOn)
	case types.ValueMethod:
		b.allow(x, actMethod, ctxMethod, ctxCheck)
	}
}

// commandCall is [x].command(...) or [x]?.command(...).
func (b *body) commandCall(x *ast.CallExpr, fun *ast.SelectorExpr, br *ast.BracketExpr) types.Type {
	b.allow(x, actCommand, ctxCommand, ctxOn, ctxMigration, ctxTest)
	if len(br.Elems) != 1 {
		b.expr(br, nil)
		b.skipArgs(x.Args)
		b.c.bag.Add(diag.E0214, rangeOf(br), textRefTarget)
		return invalid
	}
	elem := br.Elems[0]
	t := b.expr(elem, nil)
	if types.IsInvalid(t) {
		b.skipArgs(x.Args)
		return invalid
	}
	id, ok := types.Unwrap(t).(*types.ID)
	if !ok || id.Owner.Kind == types.ProjectionDecl {
		b.skipArgs(x.Args)
		b.c.bag.Add(diag.E0214, rangeOf(elem), textRefTarget)
		return invalid
	}
	b.setType(br, &types.Optional{Elem: &types.Ref{Owner: id.Owner}})
	if fun.Safe && !b.notInFake(fun, fakeSafe) {
		b.skipArgs(x.Args)
		return invalid
	}
	if isOptional(t) && !fun.Safe {
		b.c.bag.Add(diag.E0206, rangeOf(elem), b.c.quote(elem), fun.Sel.Name)
	}
	if !b.innerAt(fun, id.Owner) {
		b.skipArgs(x.Args)
		return invalid
	}
	owner, m, hidden := b.c.entityMethod(id.Owner, fun.Sel.Name, fun)
	if hidden != nil {
		b.skipArgs(x.Args)
		b.c.closed(fun.Sel, hidden.Name+"."+fun.Sel.Name, hidden.Context)
		return invalid
	}
	if m == nil || m.Kind != types.Command {
		b.skipArgs(x.Args)
		b.c.bag.Add(diag.E0221, rangeOf(fun.Sel), id.Owner.Name, diag.Message{Format: textNoCommand, Args: []any{fun.Sel.Name}})
		return invalid
	}
	kind := CallCommand
	if b.fake() {
		kind = CallExternal
		b.roleAccess(fun.Sel, owner, m)
	}
	b.c.info.Calls[x] = &Call{Kind: kind, Decl: owner, Method: m}
	b.args(x, quoteName(owner.Short+"."+m.Name), m.Params)
	return tVoid
}

// roleAccess checks that a model command a fake calls is public and open to
// its system's role (E1208).
func (b *body) roleAccess(n ast.Node, owner *types.Decl, m *types.Method) {
	role := b.owner.Role
	name := owner.Name + "." + m.Name
	if !m.Public() {
		b.c.bag.Add(diag.E1208, rangeOf(n), role, name, "the command is private")
		return
	}
	cmd, _ := m.Node.(*ast.CommandDecl)
	if cmd == nil || cmd.Access == nil {
		return
	}
	for _, id := range cmd.Access.Names {
		if a, ok := b.c.info.Access[id]; ok && a.Kind == AccessRole && b.c.info.RoleNames[a.Role] == role {
			return
		}
	}
	b.c.bag.Add(diag.E1208, rangeOf(n), role, name, "the role is not in its access list")
}

// typeCall is Type.Id(x), Type.query(...), System.request(...).
func (b *body) typeCall(x *ast.CallExpr, fun *ast.SelectorExpr, d *types.Decl) types.Type {
	name := fun.Sel.Name
	if name == "Id" && d.ID != nil {
		return b.convert(x, d)
	}
	d, m, hidden := b.c.entityMethod(d, name, fun)
	if m != nil && d.Kind != types.EntityDecl && d.Kind != types.ActorDecl && !b.c.visible(d, fun) && !m.Public() && !(m.Kind == types.Command && d.Fake && b.kind == ctxTest) {
		hidden, m = d, nil
	}
	if hidden != nil {
		b.skipArgs(x.Args)
		b.c.closed(fun.Sel, hidden.Name+"."+name, hidden.Context)
		return invalid
	}
	if m != nil && m.Kind == types.Query && b.acl {
		// An ACL does not read the source synchronously: it keeps the data it needs from events.
		b.skipArgs(x.Args)
		b.c.closed(fun.Sel, d.Name+"."+name, d.Context)
		return invalid
	}
	switch {
	case m != nil && m.Kind == types.Query && d.Kind != types.ExternalDecl:
		b.c.info.Calls[x] = &Call{Kind: CallQuery, Decl: d, Method: m}
		b.allow(x, actQuery, ctxQuery, ctxTest)
		b.notInFake(x, fakeQuery)
		b.args(x, quoteName(d.Name+"."+name), m.Params)
		return m.Result
	case m != nil && m.Kind == types.Command && d.Fake && b.kind == ctxTest:
		b.c.info.Calls[x] = &Call{Kind: CallFake, Decl: d, Method: m}
		b.args(x, quoteName(d.Name+"."+name), m.Params)
		return tVoid
	case m != nil && m.Kind == types.Request:
		b.c.info.Calls[x] = &Call{Kind: CallRequest, Decl: d, Method: m}
		b.allow(x, actRequest, ctxCommand, ctxOn)
		b.notInFake(x, fakeRequest)
		b.args(x, quoteName(d.Name+"."+name), m.Params)
		return tVoid
	}
	b.skipArgs(x.Args)
	what := textNoQuery
	if d.Kind == types.ExternalDecl {
		what = "request %q"
	}
	b.c.bag.Add(diag.E0221, rangeOf(fun.Sel), d.Name, diag.Message{Format: what, Args: []any{name}})
	return invalid
}

// convert is Type.Id(x): a conversion between a projection identifier and
// the entity identifier it is based on.
func (b *body) convert(x *ast.CallExpr, d *types.Decl) types.Type {
	b.c.info.Calls[x] = &Call{Kind: CallConvert, Decl: d}
	if len(x.Args.Args) != 1 || x.Args.Args[0].Name != nil {
		b.skipArgs(x.Args)
		b.c.bag.Add(diag.E0214, rangeOf(x), diag.Message{Format: textConvertArity, Args: []any{d.Name}})
		return d.ID
	}
	arg := x.Args.Args[0].Value
	t := b.expr(arg, nil)
	if types.IsInvalid(t) {
		return d.ID
	}
	if b.optionalOperand(arg, t) {
		return d.ID
	}
	src, _ := t.(*types.ID)
	base, _ := d.ID.Base.(*types.ID)
	switch {
	case src != nil && base != nil && base.Owner == src.Owner:
	case src != nil && identicalID(src.Base, d.ID):
	case src != nil && b.translatesID(arg, src, d):
	default:
		var allowed string
		if base != nil {
			allowed = base.String()
		} else {
			allowed = derivedNames(b.c.info, d)
		}
		b.c.bag.Add(diag.E0216, rangeOf(arg), d.Name, allowed)
	}
	return d.ID
}

func identicalID(base types.Type, id *types.ID) bool {
	bid, ok := base.(*types.ID)
	return ok && bid.Owner == id.Owner
}

// derivedNames lists the identifiers of projections based on d.
func derivedNames(info *Info, d *types.Decl) string {
	var names []string
	for _, x := range info.DeclList {
		if x.ID != nil && identicalID(x.ID.Base, d.ID) {
			names = append(names, x.Name+".Id")
		}
	}
	if len(names) == 0 {
		return "ids of projections based on it"
	}
	sort.Strings(names)
	s := strings.Join(names, ", ")
	return s
}

// methodCall is x.method(...): a value object method or a built-in method.
func (b *body) methodCall(x *ast.CallExpr, fun *ast.SelectorExpr) types.Type {
	tx := b.expr(fun.X, nil)
	recv, ok := b.receiver(fun, tx)
	if !ok {
		b.skipArgs(x.Args)
		return invalid
	}
	var t types.Type
	if v, isValue := recv.(*types.Value); isValue {
		m := v.Decl.Method(fun.Sel.Name)
		if m == nil {
			b.skipArgs(x.Args)
			b.c.bag.Add(diag.E0221, rangeOf(fun.Sel), v.Decl.Name, diag.Message{Format: textNoMethod, Args: []any{fun.Sel.Name}})
			return invalid
		}
		b.c.info.Calls[x] = &Call{Kind: CallMethod, Decl: v.Decl, Method: m}
		b.args(x, quoteName(v.Decl.Name+"."+m.Name), m.Params)
		t = m.Result
	} else {
		t = b.builtinMethod(x, fun, recv)
		if t == nil {
			return invalid
		}
		b.c.info.Calls[x] = &Call{Kind: CallBuiltinMethod, Builtin: fun.Sel.Name}
	}
	if fun.Safe && isOptional(tx) {
		return optional(t)
	}
	return t
}

// args matches named arguments to parameters (E0010, E0203–E0205). In a
// test a query is called as a client: an omitted PageRequest is size 20
// (§7.2, §27.1).
func (b *body) args(x *ast.CallExpr, what string, params []*types.Var) {
	req := required
	if b.kind == ctxTest {
		req = func(p *types.Var) bool {
			_, page := p.Type.(*types.PageReq)
			return !page && required(p)
		}
	}
	b.namedArgs(x, x.Args, what, params, req, diag.E0203)
}

// partialArgs are arguments that need not all be passed: view actions (the
// rest are form fields) and test checks.
func (b *body) partialArgs(n ast.Node, l *ast.ArgList, what string, params []*types.Var) map[string]bool {
	return b.namedArgs(n, l, what, params, func(*types.Var) bool { return false }, diag.E0203)
}

// upsertArgs are the arguments of writing a projection row: the row is
// written whole (RFC §18), so id and the fields
// without a default value are required, as for a constructor.
func (b *body) upsertArgs(x *ast.CallExpr, name string, params []*types.Var) {
	b.namedArgs(x, x.Args, quoteName(name), params, required, diag.E0305)
}

// required reports whether a parameter or field is required: there is no
// default value and the type is not optional.
func required(p *types.Var) bool {
	if isOptional(p.Type) {
		return false
	}
	switch n := p.Node.(type) {
	case *ast.Param:
		return n.Default == nil
	case *ast.FieldDecl:
		return n.Default == nil
	}
	return true
}

// namedArgs matches arguments l to parameters and returns the names of the
// passed ones; n is the node for the missingCode message about missing ones.
func (b *body) namedArgs(n ast.Node, l *ast.ArgList, what string, params []*types.Var, req func(*types.Var) bool, missingCode *diag.Spec) map[string]bool {
	byName := map[string]*types.Var{}
	for _, p := range params {
		byName[p.Name] = p
	}
	seen := map[string]bool{}
	suggested := map[string]bool{} // parameters from E0204 hints: not reported as missing
	positional := false
	if l == nil {
		l = &ast.ArgList{}
	}
	for _, a := range l.Args {
		if a.Name == nil {
			b.expr(a.Value, nil)
			if !positional {
				b.c.bag.Add(diag.E0010, rangeOf(a.Value), what)
				positional = true
			}
			continue
		}
		p := byName[a.Name.Name]
		switch {
		case p == nil:
			b.expr(a.Value, nil)
			d := b.c.bag.Add(diag.E0204, rangeOf(a.Name), what, a.Name.Name)
			var cands []string
			for _, p := range params {
				if !seen[p.Name] {
					cands = append(cands, p.Name)
				}
			}
			if s := closest(a.Name.Name, cands); s != "" {
				d.Hint(hintDidYouMean, s)
				suggested[s] = true
			}
			continue
		case seen[p.Name]:
			b.c.bag.Add(diag.E0205, rangeOf(a.Name), a.Name.Name)
		}
		seen[p.Name] = true
		b.check(a.Value, p.Type)
	}
	if positional {
		return seen
	}
	var missing []string
	for _, p := range params {
		if !seen[p.Name] && !suggested[p.Name] && req(p) {
			missing = append(missing, p.Name)
		}
	}
	if len(missing) > 0 {
		b.c.bag.Add(missingCode, rangeOf(n), what, strings.Join(missing, ", "))
	}
	return seen
}
