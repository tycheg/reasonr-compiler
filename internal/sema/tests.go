package sema

import (
	"fmt"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

var (
	textWhenCount    = "when must contain exactly one action, got %d"
	textWhenAction   = "when allows a public command, a public query or time offset"
	textNoError      = "error %q is not declared anywhere"
	textNoNotif      = "notification %q is not declared anywhere"
	textAmbiguous    = "%q is declared in several types: %s"
	textNoRequest    = "external system %q has no request %q"
	textNoSystem     = "no external system %q"
	textMatchesQuery = "matches compares a query result, but when has no query call"
	textMatchesValue = "matches compares value fields, but the query returns %s"
	textMatchesField = "%s has no field %q"
)

// builtinErrors are the built-in errors (RFC §14).
var builtinErrors = map[string]*types.Method{
	"alreadyExists": {Kind: types.Error, Name: "alreadyExists", Result: tVoid},
	"forbidden":     {Kind: types.Error, Name: "forbidden", Result: tVoid},
	"invalidArgument": {Kind: types.Error, Name: "invalidArgument", Result: tVoid, Params: []*types.Var{
		{Name: "path", Type: tString}, {Name: "rule", Type: tString}}},
	"internalError": {Kind: types.Error, Name: "internalError", Result: tVoid},
}

// checkTest checks a test (plan, §12.6): lets, given and when steps, then checks.
func (c *checker) checkTest(d *ast.TestDecl) {
	c.checkTestFeatures(d)
	f := c.fileOf(d.Pos())
	if f != nil && f.perf {
		c.checkPerfTest(d)
		return
	}
	if f != nil && f.demo {
		// A demo scenario fills a tenant: given only (RFC §27.5).
		if d.When != nil {
			c.bag.Add(diag.E0805, rangeOf(d.When), "when")
		}
		if d.Then != nil {
			c.bag.Add(diag.E0805, rangeOf(d.Then), "then")
		}
	}
	b := c.newBody(ctxTest, nil, nil)
	for _, s := range d.Setup {
		b.stmt(s)
	}
	if d.Given != nil {
		for _, s := range d.Given.Steps {
			b.testStep(s)
		}
	}
	var whenResult types.Type
	if d.When != nil {
		n := 0
		for _, s := range d.When.Steps {
			if _, ok := s.(*ast.TestStmt); !ok {
				n++
			}
		}
		if n != 1 {
			c.bag.Add(diag.E0802, rangeOf(d.When), diag.Message{Format: textWhenCount, Args: []any{n}})
		}
		for _, s := range d.When.Steps {
			call, ok := s.(*ast.TestCall)
			if !ok {
				b.testStep(s)
				continue
			}
			m := b.testCall(call)
			if m != nil && !m.Public() {
				c.bag.Add(diag.E0802, rangeOf(call.Call), textWhenAction)
			}
			if m != nil && m.Kind == types.Query {
				whenResult = m.Result
			}
		}
	}
	if d.Then != nil {
		for _, it := range d.Then.Items {
			b.thenItem(it, whenResult)
		}
	}
}

// testStep checks a given step (or a section statement): a call is a public
// command, time offset is a signed duration, a statement is as in a body.
func (b *body) testStep(s ast.TestStep) {
	switch s := s.(type) {
	case *ast.TestCall:
		if m := b.testCall(s); m != nil && (m.Kind != types.Command || !m.Public()) {
			b.c.bag.Add(diag.E0801, rangeOf(s.Call))
		}
	case *ast.TimeOffset:
		b.check(s.Duration, tDuration)
	case *ast.TestStmt:
		b.stmt(s.Stmt)
	}
}

// testCall checks a command or query call in a test and resolves as and via.
func (b *body) testCall(call *ast.TestCall) *types.Method {
	c := b.c
	var m *types.Method
	if x, ok := call.Call.(*ast.CallExpr); ok {
		// A test step is an action: then checks a query result, not a statement.
		b.setType(x, b.call0(x, nil, true))
		k := c.info.Calls[x]
		switch {
		case k != nil && (k.Kind == CallCommand || k.Kind == CallQuery):
			m = k.Method
		case k != nil && k.Kind == CallFake:
			m = k.Method
			if call.As != nil {
				c.bag.Add(diag.E1210, source.Range{Start: call.AsPos, End: call.As.End()}, k.Decl.Name+"."+k.Method.Name)
				return m
			}
		case k != nil:
			c.bag.Add(diag.E0801, rangeOf(x))
		}
	} else {
		b.expr(call.Call, nil)
	}
	if call.As != nil {
		b.testAs(call)
	}
	return m
}

// testAs resolves as: a variable with an actor identifier or a role.
func (b *body) testAs(call *ast.TestCall) {
	c := b.c
	id := call.As
	if o := b.lookupQuiet(id.Name); o != nil || c.fixtures[id.Name] != nil {
		if o == nil {
			o = c.fixtures[id.Name]
		}
		c.info.Uses[id] = o
		if t, ok := types.Unwrap(o.Type).(*types.ID); ok && t.Owner.Kind == types.ActorDecl {
			c.info.TestAs[call] = Access{Kind: AccessActor, Actor: t.Owner}
			return
		}
		if !types.IsInvalid(o.Type) {
			c.bag.Add(diag.E0803, rangeOf(id), c.quote(id))
		}
		return
	}
	if r := c.role(id, id.Name); r != nil {
		c.info.TestAs[call] = Access{Kind: AccessRole, Role: r}
		return
	}
	c.bag.Add(diag.E0803, rangeOf(id), c.quote(id))
}

func (b *body) thenItem(it ast.ThenItem, whenResult types.Type) {
	c := b.c
	switch t := it.(type) {
	case *ast.ThenError:
		m := c.findMethod(t.Name, types.Error, textNoError)
		if m == nil {
			m = builtinErrors[t.Name.Name]
		}
		if m != nil {
			c.info.Then[it] = m
			b.partialArgs(t, t.Args, quoteName(m.Name), m.Params)
		} else if t.Args != nil {
			b.skipArgs(t.Args)
		}
	case *ast.ThenNotification:
		if m := c.findMethod(t.Name, types.Notification, textNoNotif); m != nil {
			c.info.Then[it] = m
			b.partialArgs(t, t.Args, quoteName(m.Name), m.Params)
		} else if t.Args != nil {
			b.skipArgs(t.Args)
		}
		if t.To != nil {
			tt := b.expr(t.To, nil)
			if id, ok := types.Unwrap(tt).(*types.ID); !types.IsInvalid(tt) && (!ok || id.Owner.Kind != types.ActorDecl) {
				c.bag.Add(diag.E0209, rangeOf(t.To), tt.String())
			}
		}
	case *ast.ThenRequest:
		d := c.lookup(t.System, t.System.Name)
		var m *types.Method
		switch {
		case d == nil || d.Kind != types.ExternalDecl:
			c.bag.Add(diag.E0804, rangeOf(t.System), diag.Message{Format: textNoSystem, Args: []any{t.System.Name}})
		default:
			if m = d.Method(t.Name.Name); m == nil {
				c.bag.Add(diag.E0804, rangeOf(t.Name), diag.Message{Format: textNoRequest, Args: []any{d.Name, t.Name.Name}})
			}
		}
		if m != nil {
			c.info.Then[it] = m
			b.partialArgs(t, t.Args, quoteName(d.Name+"."+m.Name), m.Params)
		} else if t.Args != nil {
			b.skipArgs(t.Args)
		}
	case *ast.ThenMatches:
		result := whenResult
		if t.Call != nil {
			result = nil
			if m := b.testCall(t.Call); m != nil && m.Kind == types.Query {
				result = m.Result
			}
		} else if result == nil {
			c.bag.Add(diag.E0804, rangeOf(t), textMatchesQuery)
		}
		b.matches(t, result)
	}
}

// matches checks the fields in matches (field: value) by the result type.
func (b *body) matches(t *ast.ThenMatches, result types.Type) {
	c := b.c
	v, ok := result.(*types.Value)
	if result != nil && !ok && !types.IsInvalid(result) {
		c.bag.Add(diag.E0804, rangeOf(t.Fields), diag.Message{Format: textMatchesValue, Args: []any{result.String()}})
	}
	for _, a := range t.Fields.Args {
		if !ok || a.Name == nil {
			b.expr(a.Value, nil)
			continue
		}
		f := v.Decl.Field(a.Name.Name)
		if f == nil {
			c.bag.Add(diag.E0804, rangeOf(a.Name), diag.Message{Format: textMatchesField, Args: []any{v.Decl.Name, a.Name.Name}})
			b.expr(a.Value, nil)
			continue
		}
		b.check(a.Value, f.Type)
	}
}

// findMethod looks up an error or a notification by name in all model types.
func (c *checker) findMethod(name *ast.Ident, kind types.MethodKind, notFound string) *types.Method {
	var found []*types.Method
	var owners []string
	ms, owner := c.methodsNamed(name.Name)
	for _, m := range ms {
		if m.Kind == kind {
			found = append(found, m)
			owners = append(owners, owner[m].Name)
		}
	}
	switch len(found) {
	case 0:
		if kind == types.Error && builtinErrors[name.Name] != nil {
			return nil
		}
		c.bag.Add(diag.E0804, rangeOf(name), diag.Message{Format: notFound, Args: []any{name.Name}})
		return nil
	case 1:
		return found[0]
	}
	// The same names in different types: the check compares only the name,
	// and the parameters come from the first declaration if they match.
	for _, m := range found[1:] {
		if !sameParams(m, found[0]) {
			c.bag.Add(diag.E0804, rangeOf(name), diag.Message{Format: textAmbiguous, Args: []any{name.Name, fmt.Sprint(owners)}})
			return nil
		}
	}
	return found[0]
}

func sameParams(a, b *types.Method) bool {
	if len(a.Params) != len(b.Params) {
		return false
	}
	for i := range a.Params {
		if a.Params[i].Name != b.Params[i].Name || !identical(a.Params[i].Type, b.Params[i].Type) {
			return false
		}
	}
	return true
}

// checkPerfTest checks a performance test (RFC §27.6): given is as in
// a test, when is a user path of public commands and queries, time offset
// and let; then holds boolean expressions over stats and baseline.
func (c *checker) checkPerfTest(d *ast.TestDecl) {
	b := c.newBody(ctxTest, nil, nil)
	for _, s := range d.Setup {
		b.stmt(s)
	}
	if d.Given != nil {
		for _, s := range d.Given.Steps {
			b.testStep(s)
		}
	}
	if d.When != nil {
		for _, s := range d.When.Steps {
			call, ok := s.(*ast.TestCall)
			if !ok {
				b.testStep(s)
				continue
			}
			if m := b.testCall(call); m != nil && !m.Public() {
				c.bag.Add(diag.E0802, rangeOf(call.Call), textWhenAction)
			}
		}
	}
	if d.Then != nil {
		for _, it := range d.Then.Items {
			b.perfThen(it)
		}
	}
}
