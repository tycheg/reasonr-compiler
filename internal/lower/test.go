package lower

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// test lowers an acceptance test: shared lets, the test's lets, the given
// steps, the when mark, the when step and the then checks. The test runner
// runs the steps in order.
func (l *lowerer) test(d *ast.TestDecl, fixtures []*ast.LetStmt) {
	f := l.newFn(ir.FuncTest, d.Name.Value, nil, nil)
	nodes := []ast.Node{d}
	for _, s := range fixtures {
		nodes = append(nodes, s)
	}
	f.prologue(nodes...)
	if d.Features != nil {
		f.at(d.Features.Pos())
		for _, name := range f.l.info.TestFeatures[d] {
			f.emit(ir.Instr{Op: ir.OpTestFeature, Dst: ir.NoSlot, Sym: name})
		}
	}
	for _, s := range fixtures {
		f.stmt(s)
	}
	for _, s := range d.Setup {
		f.stmt(s)
	}
	if d.Given != nil {
		for _, s := range d.Given.Steps {
			f.testStep(s)
		}
	}
	whenResult := ir.NoOperand
	if d.When != nil {
		f.at(d.When.Pos())
	}
	f.emit(ir.Instr{Op: ir.OpTestWhen})
	if d.When != nil {
		for _, s := range d.When.Steps {
			if r := f.testStep(s); r != ir.NoOperand {
				whenResult = r
			}
		}
	}
	perf := false
	if d.Then != nil {
		for _, it := range d.Then.Items {
			if _, ok := it.(*ast.ThenExpr); ok {
				perf = true // performance test checks are functions of their own
				continue
			}
			f.thenItem(it, whenResult)
		}
	}
	l.finish(f)
	if perf {
		l.perfChecks(d)
	}
}

// testStep lowers a given or when step; for a query it returns the result slot.
func (f *fn) testStep(s ast.TestStep) ir.Operand {
	f.at(s.Pos())
	switch s := s.(type) {
	case *ast.TimeOffset:
		f.emit(ir.Instr{Op: ir.OpTestTime, Args: []ir.Operand{f.expr(s.Duration)}})
	case *ast.TestCall:
		return f.testCall(s)
	case *ast.TestStmt:
		f.stmt(s.Stmt)
	}
	return ir.NoOperand
}

// testCall is a command or query on behalf of an actor or a role. via (UI
// tests) does not go into the IR: it is a contract of the views.
func (f *fn) testCall(s *ast.TestCall) ir.Operand {
	x := s.Call.(*ast.CallExpr)
	c := f.l.info.Calls[x]
	actor, role := ir.NoOperand, ""
	if as, ok := f.l.info.TestAs[s]; ok {
		switch as.Kind {
		case sema.AccessActor:
			actor = f.expr(s.As)
		case sema.AccessRole:
			role = f.l.info.RoleNames[as.Role]
		}
	}
	sym := c.Decl.Name + "." + c.Method.Name
	if c.Kind == sema.CallQuery {
		names, args := f.args(x.Args, c.Method.Params, true)
		return f.value(irType(c.Method.Result), ir.Instr{
			Op: ir.OpTestQuery, Sym: sym, Args: append([]ir.Operand{actor}, args...), Names: names, Aux: role,
		})
	}
	target := ir.NoOperand // a fake command goes to its single instance
	if c.Kind != sema.CallFake {
		target = f.expr(x.Fun.(*ast.SelectorExpr).X.(*ast.BracketExpr).Elems[0])
	}
	names, args := f.args(x.Args, c.Method.Params, true)
	f.emit(ir.Instr{Op: ir.OpTestCmd, Sym: sym, Args: append([]ir.Operand{target, actor}, args...), Names: names, Aux: role})
	return ir.NoOperand
}

// thenItem lowers a then check: only the written arguments are passed.
func (f *fn) thenItem(it ast.ThenItem, whenResult ir.Operand) {
	f.at(it.Pos())
	switch t := it.(type) {
	case *ast.ThenNotification:
		m := f.l.info.Then[it]
		to := ir.NoOperand
		if t.To != nil {
			to = f.expr(t.To)
		}
		names, args := f.args(t.Args, m.Params, false)
		f.emit(ir.Instr{Op: ir.OpExpectNotify, Sym: f.methodSym(m), Args: append([]ir.Operand{to}, args...), Names: names})
	case *ast.ThenError:
		m := f.l.info.Then[it]
		names, args := f.args(t.Args, m.Params, false)
		f.emit(ir.Instr{Op: ir.OpExpectError, Sym: f.methodSym(m), Args: args, Names: names})
	case *ast.ThenRequest:
		m := f.l.info.Then[it]
		names, args := f.args(t.Args, m.Params, false)
		f.emit(ir.Instr{Op: ir.OpExpectReq, Sym: f.methodSym(m), Args: args, Names: names})
	case *ast.ThenMatches:
		result := whenResult
		if t.Call != nil {
			result = f.testCall(t.Call)
		}
		v := types.Unwrap(f.resultType(t, result)).(*types.Value)
		names, args := f.args(t.Fields, v.Decl.Fields, false)
		f.emit(ir.Instr{Op: ir.OpExpectMatch, Sym: v.Decl.Name, Args: append([]ir.Operand{result}, args...), Names: names})
	}
}

// resultType is the type of the query result that matches compares.
func (f *fn) resultType(t *ast.ThenMatches, result ir.Operand) types.Type {
	if t.Call != nil {
		return f.l.info.Calls[t.Call.Call.(*ast.CallExpr)].Method.Result
	}
	name := f.f.Slots[result.Slot()].Type.Unwrap().Name
	return &types.Value{Decl: f.l.info.Decls[name]}
}

// methodSym is "Type.name" of an error or a notification; the name for built-in errors.
func (f *fn) methodSym(m *types.Method) string {
	if d := f.l.ownerOf(m); d != nil {
		return d.Name + "." + m.Name
	}
	return m.Name
}
