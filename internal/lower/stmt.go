package lower

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/token"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

func (f *fn) block(b *ast.Block) {
	if b == nil {
		return
	}
	for _, s := range b.List {
		f.stmt(s)
	}
}

func (f *fn) stmt(s ast.Stmt) {
	if _, ok := s.(*ast.Block); !ok {
		f.at(s.Pos())
	}
	mark := len(f.active)
	defer f.release(mark)
	switch s := s.(type) {
	case *ast.TestCall, *ast.TimeOffset: // a step in a statement block of a test section
		f.testStep(s.(ast.TestStep))
	case *ast.Block:
		f.block(s)
	case *ast.LetStmt:
		f.let(s)
	case *ast.AssignStmt:
		id := s.Target.(*ast.Ident)
		o := f.l.info.Uses[id]
		v := f.expr(s.Value)
		if o.Kind == sema.ObjField {
			f.emit(ir.Instr{Op: ir.OpSetField, Sym: id.Name, Args: []ir.Operand{v}})
			return
		}
		f.assign(f.slots[o.Node], v)
	case *ast.IfStmt:
		f.ifStmt(s)
	case *ast.ForStmt:
		f.forStmt(s)
	case *ast.WhileStmt:
		head, body, exit := f.newBlock(), f.newBlock(), f.newBlock()
		f.jmp(head)
		f.setBlock(head)
		f.emit(ir.Instr{Op: ir.OpLoopGuard})
		f.cond(s.Cond, body, exit)
		f.setBlock(body)
		f.loops = append(f.loops, loop{brk: exit, cont: head})
		f.block(s.Body)
		f.loops = f.loops[:len(f.loops)-1]
		f.jmp(head)
		f.setBlock(exit)
	case *ast.BranchStmt:
		lp := f.loops[len(f.loops)-1]
		if s.Tok == token.BREAK {
			f.jmp(lp.brk)
		} else {
			f.jmp(lp.cont)
		}
	case *ast.ReturnStmt:
		if s.Result == nil {
			f.ret(ir.NoOperand)
		} else {
			f.ret(f.expr(s.Result))
		}
	case *ast.ExprStmt:
		f.exprStmt(s)
	case *ast.DeleteStmt:
		f.deleteStmt(s)
	case *ast.ForgetStmt:
		id := types.Unwrap(f.l.info.Types[s.X]).(*types.ID)
		f.emit(ir.Instr{Op: ir.OpForget, Sym: id.Owner.Name, Args: []ir.Operand{f.expr(s.X)}})
	}
}

func (f *fn) let(s *ast.LetStmt) {
	o := f.l.info.Uses[s.Name]
	v := f.expr(s.Value)
	f.assign(f.local(o.Node, s.Name.Name, irType(o.Type)), v)
}

func (f *fn) ifStmt(s *ast.IfStmt) {
	then, done := f.newBlock(), f.newBlock()
	els := done
	if s.Else != nil {
		els = f.newBlock()
	}
	f.cond(s.Cond, then, els)
	f.setBlock(then)
	f.block(s.Body)
	f.jmp(done)
	if s.Else != nil {
		f.setBlock(els)
		f.stmt(s.Else)
		f.jmp(done)
	}
	f.setBlock(done)
}

// forStmt is a loop by index: for x in xs; a map iterates over its keys.
func (f *fn) forStmt(s *ast.ForStmt) {
	xs := f.expr(s.X)
	t := f.l.info.Types[s.X]
	var elem *ir.Type
	switch u := t.(type) {
	case *types.Map:
		keys := &ir.Type{Kind: ir.TList, Elem: irType(u.Key)}
		xs = f.value(keys, ir.Instr{Op: ir.OpIntrinsic, Sym: "map.keys", Args: []ir.Operand{xs}})
		elem = irType(u.Key)
	default:
		elem = irType(elemOf(t))
	}
	n := f.value(ir.Basic(ir.TInt), ir.Instr{Op: ir.OpLen, Args: []ir.Operand{xs}})
	i := f.temp(ir.Basic(ir.TInt))
	f.emit(ir.Instr{Op: ir.OpMove, Dst: i, Args: []ir.Operand{f.konst(intConst(0))}})
	head, body, next, exit := f.newBlock(), f.newBlock(), f.newBlock(), f.newBlock()
	f.jmp(head)
	f.setBlock(head)
	f.emit(ir.Instr{Op: ir.OpLoopGuard})
	c := f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.MustLookup("lt.i64"), Args: []ir.Operand{ir.SlotOp(i), n}})
	f.br(c, body, exit)
	f.setBlock(body)
	o := f.l.info.Uses[s.Var]
	x := f.local(o.Node, s.Var.Name, elem)
	f.emit(ir.Instr{Op: ir.OpGet, Dst: x, Args: []ir.Operand{xs, ir.SlotOp(i)}})
	f.loops = append(f.loops, loop{brk: exit, cont: next})
	f.block(s.Body)
	f.loops = f.loops[:len(f.loops)-1]
	f.jmp(next)
	f.setBlock(next)
	f.emit(ir.Instr{Op: ir.MustLookup("add.i64"), Dst: i, Args: []ir.Operand{ir.SlotOp(i), f.konst(intConst(1))}})
	f.jmp(head)
	f.setBlock(exit)
}

func elemOf(t types.Type) types.Type {
	switch t := t.(type) {
	case *types.List:
		return t.Elem
	case *types.Set:
		return t.Elem
	case *types.Page:
		return t.Elem
	}
	return nil
}

func (f *fn) deleteStmt(s *ast.DeleteStmt) {
	if br, ok := s.X.(*ast.BracketExpr); ok {
		// delete [projection row]: removed by identifier.
		id := types.Unwrap(f.l.info.Types[br.Elems[0]]).(*types.ID)
		f.emit(ir.Instr{Op: ir.OpProjDelete, Sym: id.Owner.Name, Args: []ir.Operand{f.expr(br.Elems[0])}})
		return
	}
	f.emit(ir.Instr{Op: ir.OpTimerDelete, Args: []ir.Operand{f.expr(s.X)}})
}

// cond lowers a condition into jumps: and and or are evaluated lazily.
func (f *fn) cond(x ast.Expr, t, e int) {
	switch c := x.(type) {
	case *ast.ParenExpr:
		f.cond(c.X, t, e)
		return
	case *ast.UnaryExpr:
		if c.Op == token.NOT {
			f.cond(c.X, e, t)
			return
		}
	case *ast.BinaryExpr:
		switch c.Op {
		case token.AND:
			mid := f.newBlock()
			f.cond(c.X, mid, e)
			f.setBlock(mid)
			f.cond(c.Y, t, e)
			return
		case token.OR:
			mid := f.newBlock()
			f.cond(c.X, t, mid)
			f.setBlock(mid)
			f.cond(c.Y, t, e)
			return
		}
	case *ast.IsNoneExpr:
		v := f.expr(c.X)
		if _, ok := f.constOf(v); !ok {
			none := f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpIsNone, Args: []ir.Operand{v}})
			if c.Not {
				t, e = e, t
			}
			f.br(none, t, e)
			return
		}
	case *ast.BracketExpr:
		// if [x]: whether the entity or record exists.
		if typ := f.l.info.Types[c]; len(c.Elems) == 1 && isOptional(typ) {
			switch r := types.Unwrap(typ).(type) {
			case *types.Ref:
				v := f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpRefExists, Sym: r.Owner.Name, Args: []ir.Operand{f.expr(c.Elems[0])}})
				f.br(v, t, e)
				return
			case *types.Record:
				rec := f.expr(c)
				none := f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpIsNone, Args: []ir.Operand{rec}})
				f.br(none, e, t)
				return
			}
		}
	}
	v := f.expr(x)
	if k, ok := f.constOf(v); ok {
		if k.Text == "true" {
			f.jmp(t)
		} else {
			f.jmp(e)
		}
		return
	}
	f.br(v, t, e)
}

// boolValue evaluates a condition into a bool value through jumps.
func (f *fn) boolValue(x ast.Expr) ir.Operand {
	res := f.temp(ir.Basic(ir.TBool))
	t, e, done := f.newBlock(), f.newBlock(), f.newBlock()
	f.cond(x, t, e)
	f.setBlock(t)
	f.emit(ir.Instr{Op: ir.OpMove, Dst: res, Args: []ir.Operand{f.konst(boolConst(true))}})
	f.jmp(done)
	f.setBlock(e)
	f.emit(ir.Instr{Op: ir.OpMove, Dst: res, Args: []ir.Operand{f.konst(boolConst(false))}})
	f.jmp(done)
	f.setBlock(done)
	return ir.SlotOp(res)
}

func isOptional(t types.Type) bool {
	_, ok := t.(*types.Optional)
	return ok
}
