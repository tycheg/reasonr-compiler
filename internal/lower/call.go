package lower

import (
	"fmt"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/token"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// exprStmt is a call as a separate statement: effects and commands.
func (f *fn) exprStmt(s *ast.ExprStmt) {
	x := s.X.(*ast.CallExpr)
	c := f.l.info.Calls[x]
	if c.Kind == sema.CallMethod && c.Method.Kind == types.Notification {
		to := ir.NoOperand
		if s.To != nil {
			to = f.expr(s.To)
		}
		names, args := f.args(x.Args, c.Method.Params, true)
		f.emit(ir.Instr{Op: ir.OpNotify, Sym: c.Decl.Name + "." + c.Method.Name, Args: append([]ir.Operand{to}, args...), Names: names, Subjects: f.subjects(x)})
		return
	}
	f.call(x)
}

// call lowers a call. Calls without a result (commands, events, effects)
// return NoOperand.
func (f *fn) call(x *ast.CallExpr) ir.Operand {
	c := f.l.info.Calls[x]
	if c == nil {
		panic("lower: the call is not resolved")
	}
	sym := func() string { return c.Decl.Name + "." + c.Method.Name }
	switch c.Kind {
	case sema.CallMethod:
		switch c.Method.Kind {
		case types.Command:
			names, args := f.args(x.Args, c.Method.Params, true)
			self := f.selfID()
			f.emit(ir.Instr{Op: ir.OpCmd, Sym: sym(), Args: append([]ir.Operand{self}, args...), Names: names})
			return ir.NoOperand
		case types.Event:
			names, args := f.args(x.Args, c.Method.Params, true)
			f.emit(ir.Instr{Op: ir.OpEvent, Sym: sym(), Args: args, Names: names})
			return ir.NoOperand
		case types.Notification:
			names, args := f.args(x.Args, c.Method.Params, true)
			f.emit(ir.Instr{Op: ir.OpNotify, Sym: sym(), Args: append([]ir.Operand{ir.NoOperand}, args...), Names: names, Subjects: f.subjects(x)})
			return ir.NoOperand
		case types.Error:
			names, args := f.args(x.Args, c.Method.Params, true)
			f.emit(ir.Instr{Op: ir.OpError, Sym: sym(), Args: args, Names: names, Subjects: f.subjects(x)})
			return ir.NoOperand
		case types.Rule:
			names, args := f.args(x.Args, c.Method.Params, true)
			return f.value(irType(c.Method.Result), ir.Instr{Op: ir.OpRuleCall, Sym: sym(), Args: args, Names: names})
		case types.ValueMethod:
			return f.valueMethod(x, c)
		}
	case sema.CallCommand:
		fun := x.Fun.(*ast.SelectorExpr)
		target := f.expr(fun.X.(*ast.BracketExpr).Elems[0])
		names, args := f.args(x.Args, c.Method.Params, true)
		op := ir.OpCmd
		if fun.Safe {
			op = ir.OpCmdOpt
		}
		f.emit(ir.Instr{Op: op, Sym: sym(), Args: append([]ir.Operand{target}, args...), Names: names})
		return ir.NoOperand
	case sema.CallExternal:
		target := f.expr(x.Fun.(*ast.SelectorExpr).X.(*ast.BracketExpr).Elems[0])
		names, args := f.args(x.Args, c.Method.Params, true)
		f.emit(ir.Instr{Op: ir.OpCallOut, Sym: sym(), Args: append([]ir.Operand{target}, args...), Names: names})
		return ir.NoOperand
	case sema.CallQuery:
		names, args := f.args(x.Args, c.Method.Params, true)
		return f.value(irType(c.Method.Result), ir.Instr{Op: ir.OpQueryCall, Sym: sym(), Args: args, Names: names})
	case sema.CallRequest:
		names, args := f.args(x.Args, c.Method.Params, true)
		f.emit(ir.Instr{Op: ir.OpRequest, Sym: sym(), Args: args, Names: names, Subjects: f.subjects(x)})
		return ir.NoOperand
	case sema.CallConstruct:
		names, args := f.args(x.Args, c.Decl.Fields, true)
		return f.value(&ir.Type{Kind: ir.TValue, Name: c.Decl.Name}, ir.Instr{Op: ir.OpValNew, Sym: c.Decl.Name, Args: args, Names: names})
	case sema.CallUpsert:
		params := append([]*types.Var{{Name: "id", Type: c.Decl.ID}}, c.Decl.Fields...)
		names, args := f.args(x.Args, params, false)
		f.emit(ir.Instr{Op: ir.OpUpsert, Sym: c.Decl.Name, Args: args, Names: names})
		return ir.NoOperand
	case sema.CallEnumConvert:
		return f.enumConvert(x.Args.Args[0].Value, c.Decl)
	case sema.CallConvert:
		v := f.expr(x.Args.Args[0].Value)
		return f.value(irType(c.Decl.ID), ir.Instr{Op: ir.OpIDConv, Sym: c.Decl.Name, Args: []ir.Operand{v}})
	case sema.CallBuiltin:
		if c.Builtin == "schedule" {
			return f.schedule(x)
		}
		var args []ir.Operand
		for _, a := range x.Args.Args {
			args = append(args, f.expr(a.Value))
		}
		return f.value(irType(f.typeOf(x)), ir.Instr{Op: ir.OpIntrinsic, Sym: c.Builtin, Args: args})
	case sema.CallBuiltinMethod:
		fun := x.Fun.(*ast.SelectorExpr)
		recv := types.Unwrap(f.typeOf(fun.X))
		t := irType(f.typeOf(x))
		call := func(v ir.Operand) ir.Operand {
			args := []ir.Operand{v}
			for _, a := range x.Args.Args {
				args = append(args, f.expr(a.Value))
			}
			return f.value(t, ir.Instr{Op: ir.OpIntrinsic, Sym: recvKind(recv) + "." + c.Builtin, Args: args})
		}
		v := f.expr(fun.X)
		if fun.Safe {
			return f.safe(v, t, call)
		}
		return call(v)
	}
	panic(fmt.Sprintf("lower: a call of kind %d is not lowered", c.Kind))
}

// selfID is the identifier of the current entity (a call of its own command).
func (f *fn) selfID() ir.Operand {
	if s, ok := f.builtins["id"]; ok {
		return ir.SlotOp(s)
	}
	return f.value(irType(f.owner.ID), ir.Instr{Op: ir.OpID})
}

// valueMethod is a value object method: x.method(…), or method(…) inside
// another method of the same type (the receiver is self).
func (f *fn) valueMethod(x *ast.CallExpr, c *sema.Call) ir.Operand {
	sym := c.Decl.Name + "." + c.Method.Name
	t := irType(f.typeOf(x))
	call := func(recv ir.Operand) ir.Operand {
		names, args := f.args(x.Args, c.Method.Params, true)
		return f.value(irType(c.Method.Result), ir.Instr{Op: ir.OpMethod, Sym: sym, Args: append([]ir.Operand{recv}, args...), Names: names})
	}
	fun, ok := x.Fun.(*ast.SelectorExpr)
	if !ok {
		self := f.value(&ir.Type{Kind: ir.TValue, Name: c.Decl.Name}, ir.Instr{Op: ir.OpSelf})
		return call(self)
	}
	v := f.expr(fun.X)
	if fun.Safe {
		return f.safe(v, t, call)
	}
	return call(v)
}

// args evaluates arguments in written order and returns them in parameter
// order. full: omitted parameters get their default value or none;
// otherwise only the written arguments are passed.
func (f *fn) args(l *ast.ArgList, params []*types.Var, full bool) ([]string, []ir.Operand) {
	passed := map[string]ir.Operand{}
	if l != nil {
		for _, a := range l.Args {
			passed[a.Name.Name] = f.expr(a.Value)
		}
	}
	var names []string
	var ops []ir.Operand
	for _, p := range params {
		v, ok := passed[p.Name]
		if !ok {
			if !full {
				continue
			}
			v = f.defaultOf(p)
		}
		names = append(names, p.Name)
		ops = append(ops, v)
	}
	return names, ops
}

// subjects are the personal data subjects of call arguments (check.PII).
func (f *fn) subjects(x *ast.CallExpr) []ir.Subject {
	ss := f.l.sites[x]
	if len(ss) == 0 {
		return nil
	}
	out := make([]ir.Subject, len(ss))
	for i, s := range ss {
		out[i] = ir.Subject{Arg: s.Arg, Type: s.Subject.Name, Path: s.Path}
	}
	return out
}

// defaultOf is the value of an omitted argument: "= value" or none.
func (f *fn) defaultOf(p *types.Var) ir.Operand {
	var def ast.Expr
	switch n := p.Node.(type) {
	case *ast.Param:
		def = n.Default
	case *ast.FieldDecl:
		def = n.Default
	}
	if def != nil {
		return f.expr(def)
	}
	return f.konst(ir.Const{Type: ir.Optional(irType(types.Unwrap(p.Type))), None: true})
}

// schedule(at: …, run: command(…)) is a timer for a command of its own entity.
func (f *fn) schedule(x *ast.CallExpr) ir.Operand {
	var at ir.Operand
	var run *ast.CallExpr
	for _, a := range x.Args.Args {
		switch a.Name.Name {
		case "at":
			at = f.expr(a.Value)
		case "run":
			run = a.Value.(*ast.CallExpr)
		}
	}
	c := f.l.info.Calls[run]
	names, args := f.args(run.Args, c.Method.Params, true)
	return f.value(ir.Basic(ir.TTimer), ir.Instr{
		Op: ir.OpSchedule, Sym: c.Decl.Name + "." + c.Method.Name, Args: append([]ir.Operand{at}, args...), Names: names,
	})
}

var condOps = map[token.Kind]ir.CondOp{
	token.EQ: ir.CondEq, token.LT: ir.CondLt, token.LE: ir.CondLe, token.GT: ir.CondGt, token.GE: ir.CondGe, token.IN: ir.CondIn,
}

// search is a search [Type] where … sort … page|limit … select …: a
// FindSpec template, then a select loop over the found records.
func (f *fn) search(x *ast.SearchExpr) ir.Operand {
	sr := f.l.searches[x]
	if sr == nil {
		panic("lower: the search is not parsed")
	}
	d := f.l.info.TypeRefs[x]
	spec := &ir.FindSpec{Type: d.Name, Page: ir.NoOperand, Limit: ir.NoOperand}
	if sr.Index != nil {
		spec.Index = sr.Index.Name
	}
	for _, c := range sr.Conds {
		var v ir.Operand
		if c.Value == nil {
			v = f.konst(ir.Const{Type: ir.Optional(irType(types.Unwrap(pathType(d, c.Path)))), None: true})
		} else {
			v = f.expr(c.Value)
		}
		spec.Where = append(spec.Where, ir.Cond{Path: c.Path, Op: condOps[c.Op], Value: v})
	}
	for _, k := range x.Sort {
		var path []string
		for _, e := range k.Path.Elems {
			path = append(path, e.Name)
		}
		spec.Sort = append(spec.Sort, ir.SortKey{Path: path, Desc: k.Desc()})
	}
	if x.Page != nil {
		spec.Page = f.expr(x.Page)
	}
	if x.Limit != nil {
		spec.Limit = f.expr(x.Limit)
	}
	rec := &ir.Type{Kind: ir.TRecord, Name: d.Name}
	found := &ir.Type{Kind: ir.TList, Elem: rec}
	if x.Page != nil {
		found = &ir.Type{Kind: ir.TPage, Elem: rec}
	}
	rows := f.value(found, ir.Instr{Op: ir.OpFind, Find: spec})
	if x.Select == nil {
		return rows
	}
	// select: a new list of expressions over each record.
	items := rows
	if x.Page != nil {
		items = f.value(&ir.Type{Kind: ir.TList, Elem: rec}, ir.Instr{Op: ir.OpIntrinsic, Sym: "page.items", Args: []ir.Operand{rows}})
	}
	elem := irType(f.typeOf(x.Select))
	acc := f.temp(&ir.Type{Kind: ir.TList, Elem: elem})
	f.emit(ir.Instr{Op: ir.OpCollList, Dst: acc})
	n := f.value(ir.Basic(ir.TInt), ir.Instr{Op: ir.OpLen, Args: []ir.Operand{items}})
	i := f.temp(ir.Basic(ir.TInt))
	f.emit(ir.Instr{Op: ir.OpMove, Dst: i, Args: []ir.Operand{f.konst(intConst(0))}})
	head, body, exit := f.newBlock(), f.newBlock(), f.newBlock()
	f.jmp(head)
	f.setBlock(head)
	f.emit(ir.Instr{Op: ir.OpLoopGuard})
	c := f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.MustLookup("lt.i64"), Args: []ir.Operand{ir.SlotOp(i), n}})
	f.br(c, body, exit)
	f.setBlock(body)
	row := f.value(rec, ir.Instr{Op: ir.OpGet, Args: []ir.Operand{items, ir.SlotOp(i)}})
	f.rows = append(f.rows, row)
	v := f.expr(x.Select)
	f.rows = f.rows[:len(f.rows)-1]
	f.emit(ir.Instr{Op: ir.OpAppend, Dst: acc, Args: []ir.Operand{ir.SlotOp(acc), v}})
	f.emit(ir.Instr{Op: ir.MustLookup("add.i64"), Dst: i, Args: []ir.Operand{ir.SlotOp(i), f.konst(intConst(1))}})
	f.jmp(head)
	f.setBlock(exit)
	if x.Page == nil {
		return ir.SlotOp(acc)
	}
	return f.value(irType(f.typeOf(x)), ir.Instr{Op: ir.OpPageWith, Args: []ir.Operand{rows, ir.SlotOp(acc)}})
}

// pathType is the type of a record field by a path through value objects.
func pathType(d *types.Decl, path []string) types.Type {
	var t types.Type
	owner := d
	for _, name := range path {
		if name == "id" && owner.ID != nil {
			t = owner.ID
			continue
		}
		fl := owner.Field(name)
		if fl == nil {
			panic("lower: no field " + name)
		}
		t = fl.Type
		if v, ok := types.Unwrap(t).(*types.Value); ok {
			owner = v.Decl
		}
	}
	return t
}

// enumConvert converts an enum value of another bounded context by name: a
// chain of comparisons with each value of the source enum.
func (f *fn) enumConvert(arg ast.Expr, dst *types.Decl) ir.Operand {
	src := types.Unwrap(f.typeOf(arg)).(*types.Enum).Decl
	v := f.expr(arg)
	st := &ir.Type{Kind: ir.TEnum, Name: src.Name}
	dt := &ir.Type{Kind: ir.TEnum, Name: dst.Name}
	res := f.temp(dt)
	done := f.newBlock()
	for i, sv := range src.Values {
		to := f.konst(ir.Const{Type: dt, Text: sv.Name})
		if i == len(src.Values)-1 {
			f.emit(ir.Instr{Op: ir.OpMove, Dst: res, Args: []ir.Operand{to}})
			f.jmp(done)
			break
		}
		eq := f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.MustLookup("eq.enum"), Args: []ir.Operand{v, f.konst(ir.Const{Type: st, Text: sv.Name})}})
		yes, next := f.newBlock(), f.newBlock()
		f.br(eq, yes, next)
		f.setBlock(yes)
		f.emit(ir.Instr{Op: ir.OpMove, Dst: res, Args: []ir.Operand{to}})
		f.jmp(done)
		f.setBlock(next)
	}
	f.setBlock(done)
	return ir.SlotOp(res)
}
