package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

var (
	textBreakOutside  = "break and continue are only allowed inside a loop"
	textReturnValue   = "return takes no value here: commands, reactions and migrations return nothing"
	textToNotNotif    = "'to' is only allowed after a notification call"
	textAssignParam   = "%q cannot be changed: only let variables can be assigned"
	textForgetKind    = "forget takes an entity or actor id, the subject of personal data"
	textUnreachableIf = "a branch that ends on every path"
	actDelete         = "delete timers or projection rows"
	actForget         = "forget subjects"
)

// block checks a block in a new scope. It returns true if the block surely
// ends (return, break, continue on all paths).
func (b *body) block(blk *ast.Block) bool {
	b.push()
	defer b.pop()
	return b.stmts(blk.List)
}

func (b *body) stmts(list []ast.Stmt) bool {
	terminated := false
	var last ast.Stmt
	for _, s := range list {
		if terminated {
			b.c.bag.Add(diag.W1002, source.Range{Start: s.Pos(), End: list[len(list)-1].End()}, terminator(last))
			// The rest is still checked but does not affect the flow.
			saved, savedErr := b.facts.clone(), b.errored
			for _, rest := range list[indexOf(list, s):] {
				b.stmt(rest)
			}
			b.facts, b.errored = saved, savedErr
			return true
		}
		if b.stmt(s) {
			terminated, last = true, s
		}
	}
	return terminated
}

func indexOf(list []ast.Stmt, s ast.Stmt) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return len(list)
}

func terminator(s ast.Stmt) any {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return "return"
	case *ast.BranchStmt:
		return s.Tok.String()
	}
	return textUnreachableIf
}

// stmt checks a statement. It returns true if it ends the path.
func (b *body) stmt(s ast.Stmt) bool {
	if b.kind == ctxEvent {
		if _, ok := s.(*ast.AssignStmt); !ok {
			b.c.bag.Add(diag.E0302, rangeOf(s))
			return false
		}
	}
	switch s := s.(type) {
	case *ast.TestCall: // a step in a statement block of a test section
		b.testStep(s)
	case *ast.TimeOffset:
		b.testStep(s)
	case *ast.LetStmt:
		b.let(s)
	case *ast.AssignStmt:
		b.assign(s)
	case *ast.IfStmt:
		return b.ifStmt(s)
	case *ast.ForStmt:
		b.loop(s.Body, func() {
			t := b.expr(s.X, nil)
			elem := types.Type(invalid)
			switch u := t.(type) {
			case *types.Invalid:
			case *types.Map:
				elem = u.Key
			default:
				if e := elemOf(t); e != nil {
					elem = e
				} else {
					b.c.bag.Add(diag.E0220, rangeOf(s.X), "for … in", t.String())
				}
			}
			b.push()
			b.declare(&Object{Kind: ObjLocal, Name: s.Var.Name, Type: elem, Node: s.Var})
			b.c.info.Uses[s.Var] = b.scopes[len(b.scopes)-1][s.Var.Name]
		}, func() { b.pop() })
	case *ast.WhileStmt:
		b.loop(s.Body, func() {
			t, _ := b.cond(s.Cond)
			b.facts = b.facts.with(t)
		}, nil)
	case *ast.BranchStmt:
		if b.loops == 0 {
			b.c.bag.Add(diag.E0214, rangeOf(s), textBreakOutside)
		}
		return true
	case *ast.ReturnStmt:
		b.returnStmt(s)
		return true
	case *ast.ExprStmt:
		b.exprStmt(s)
	case *ast.DeleteStmt:
		b.allow(s, actDelete, ctxCommand, ctxOn)
		t := b.expr(s.X, nil)
		switch u := types.Unwrap(t).(type) {
		case *types.Invalid, *types.TimerID:
		case *types.Ref:
			if u.Owner.Kind != types.ProjectionDecl {
				b.c.bag.Add(diag.E0208, rangeOf(s.X), t.String())
			}
		default:
			b.c.bag.Add(diag.E0208, rangeOf(s.X), t.String())
		}
	case *ast.ForgetStmt:
		b.allow(s, actForget, ctxCommand, ctxOn)
		b.notInFake(s, fakeForget)
		t := b.expr(s.X, nil)
		if id, ok := types.Unwrap(t).(*types.ID); !types.IsInvalid(t) && (!ok || id.Owner.Kind == types.ProjectionDecl) {
			b.c.bag.Add(diag.E0214, rangeOf(s.X), textForgetKind)
		}
	}
	return false
}

// allow checks that the action is allowed in the current body kind.
func (b *body) allow(n ast.Node, act string, kinds ...ctxKind) bool {
	for _, k := range kinds {
		if b.kind == k {
			return true
		}
	}
	b.c.bag.Add(diag.E0303, rangeOf(n), ctxNames[b.kind], act)
	return false
}

func (b *body) let(s *ast.LetStmt) types.Type {
	var t types.Type
	if s.Type != nil {
		t = b.c.resolveType(s.Type)
		b.check(s.Value, t)
	} else {
		t = b.expr(s.Value, nil)
		switch t.(type) {
		case *types.None:
			b.c.bag.Add(diag.E0218, rangeOf(s.Value), "none")
			t = invalid
		case *types.Void:
			t = invalid
		}
	}
	o := &Object{Kind: ObjLocal, Name: s.Name.Name, Type: t, Node: s.Name}
	b.declare(o)
	b.c.info.Uses[s.Name] = o
	return t
}

func (b *body) assign(s *ast.AssignStmt) {
	id, ok := s.Target.(*ast.Ident)
	if !ok {
		b.expr(s.Value, nil)
		return
	}
	o := b.lookup(id)
	if o == nil {
		b.unknown(id)
		b.expr(s.Value, nil)
		return
	}
	b.c.info.Uses[id] = o
	switch o.Kind {
	case ObjLocal:
		b.check(s.Value, o.Type)
		b.facts.forget(b.key(id))
	case ObjField:
		if b.kind != ctxEvent || b.record != nil {
			b.c.bag.Add(diag.E0301, rangeOf(id), id.Name)
		}
		b.check(s.Value, o.Type)
	default:
		b.c.bag.Add(diag.E0214, rangeOf(id), diag.Message{Format: textAssignParam, Args: []any{id.Name}})
		b.expr(s.Value, nil)
	}
}

func (b *body) ifStmt(s *ast.IfStmt) bool {
	reads := b.flagReads
	b.flagReads = nil
	whenTrue, whenFalse := b.cond(s.Cond)
	flag := b.condFlag(b.flagReads)
	b.flagReads = reads
	if flag != nil && b.flag == nil {
		b.flag = flag
		defer func() { b.flag = nil }()
	}
	saved, savedErr := b.facts, b.errored

	b.facts = saved.with(whenTrue)
	thenTerm := b.block(s.Body)
	thenFacts, thenErr := b.facts, b.errored

	b.facts, b.errored = saved.with(whenFalse), savedErr
	elseTerm := false
	switch e := s.Else.(type) {
	case *ast.IfStmt:
		elseTerm = b.ifStmt(e)
	case *ast.Block:
		elseTerm = b.block(e)
	}
	elseFacts, elseErr := b.facts, b.errored

	switch {
	case thenTerm && elseTerm:
		b.facts, b.errored = saved, savedErr
		return true
	case thenTerm:
		b.facts, b.errored = elseFacts, elseErr
	case elseTerm:
		b.facts, b.errored = thenFacts, thenErr
	default:
		b.facts, b.errored = intersect(thenFacts, elseFacts), thenErr && elseErr
	}
	return false
}

// loop checks a loop body. Facts about variables the body assigns are
// dropped before the body and after the loop: the body may run any number
// of times.
func (b *body) loop(blk *ast.Block, head, after func()) {
	assigned := b.assignedIn(blk)
	for _, k := range assigned {
		b.facts.forget(k)
	}
	saved, savedErr := b.facts.clone(), b.errored
	head()
	b.loops++
	b.block(blk)
	b.loops--
	if after != nil {
		after()
	}
	b.facts, b.errored = saved, savedErr
	for _, k := range assigned {
		b.facts.forget(k)
	}
}

// assignedIn returns the keys of the variables assigned in a block.
func (b *body) assignedIn(blk *ast.Block) []string {
	var keys []string
	ast.Inspect(blk, func(n ast.Node) bool {
		if s, ok := n.(*ast.AssignStmt); ok {
			if id, ok := s.Target.(*ast.Ident); ok {
				if o := b.lookupQuiet(id.Name); o != nil && o.Kind == ObjLocal {
					keys = append(keys, keyOf(o))
				}
			}
		}
		return true
	})
	return keys
}

// lookupQuiet looks up a variable by name without diagnostics.
func (b *body) lookupQuiet(name string) *Object {
	for i := len(b.scopes) - 1; i >= 0; i-- {
		if o := b.scopes[i][name]; o != nil {
			return o
		}
	}
	return nil
}

func (b *body) returnStmt(s *ast.ReturnStmt) {
	switch b.kind {
	case ctxQuery:
		if s.Result != nil {
			b.check(s.Result, b.result)
		} else if !b.errored {
			b.c.bag.Add(diag.E0507, rangeOf(s))
		}
	case ctxRule, ctxMethod:
		if s.Result != nil {
			b.check(s.Result, b.result)
		} else {
			what := "a rule"
			if b.kind == ctxMethod {
				what = "a method"
			}
			b.c.bag.Add(diag.E0508, rangeOf(s), what)
		}
	default:
		if s.Result != nil {
			b.expr(s.Result, nil)
			b.c.bag.Add(diag.E0214, rangeOf(s.Result), textReturnValue)
		}
	}
}

func (b *body) exprStmt(s *ast.ExprStmt) {
	call, ok := s.X.(*ast.CallExpr)
	if !ok {
		b.expr(s.X, nil)
		return
	}
	b.call(call, nil, true)
	c := b.c.info.Calls[call]
	if c != nil && c.Kind == CallMethod && c.Method.Kind == types.Error {
		b.errored = true
	}
	if s.To == nil {
		return
	}
	if c == nil || c.Method == nil || c.Method.Kind != types.Notification {
		if c != nil {
			b.c.bag.Add(diag.E0214, source.Range{Start: s.ToPos, End: s.To.End()}, textToNotNotif)
		}
		b.expr(s.To, nil)
		return
	}
	t := b.expr(s.To, nil)
	if id, ok := types.Unwrap(t).(*types.ID); !types.IsInvalid(t) && (!ok || id.Owner.Kind != types.ActorDecl) {
		b.c.bag.Add(diag.E0209, rangeOf(s.To), t.String())
	}
}

func keyOf(o *Object) string {
	if o.Node != nil {
		return nodeKey(o.Node)
	}
	return "field:" + o.Name
}

// isUntyped reports whether an expression is a numeric literal whose type
// the context sets.
func isUntyped(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.BasicLit:
		return x.Kind == token.INT || x.Kind == token.DECIMAL || x.Kind == token.NONE
	case *ast.ParenExpr:
		return isUntyped(x.X)
	case *ast.UnaryExpr:
		return x.Op == token.SUB && isUntyped(x.X)
	}
	return false
}
