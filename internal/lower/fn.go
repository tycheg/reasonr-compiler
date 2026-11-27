package lower

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// fn is the lowering state of one function.
type fn struct {
	l     *lowerer
	f     *ir.Func
	owner *types.Decl
	cur   int // the current block

	slots    map[ast.Node]int // parameters, variables and fixtures by declaration
	builtins map[string]int   // id, now, today, me, caller: evaluated at the start
	consts   map[string]int   // constant key → index in the pool

	// Temporary slots live until the end of a language statement: then they
	// can be reused (plan, §13.5).
	temps  map[int]bool
	active []int
	free   map[string][]int

	rows   []ir.Operand // search records for select (nesting)
	loops  []loop
	reacts bool // on reactions are called before ret

	file, line int // position of the current statement (Instr.File, Instr.Line)
}

// at sets the statement position for the following instructions.
func (f *fn) at(pos source.Pos) {
	if file, line := f.l.position(pos); line > 0 {
		f.file, f.line = file, line
	}
}

type loop struct{ brk, cont int }

func (l *lowerer) newFn(kind ir.FuncKind, name string, owner *types.Decl, params []*types.Var) *fn {
	f := &fn{
		l: l, owner: owner,
		f:     &ir.Func{Kind: kind, Name: name, Blocks: []*ir.Block{{}}},
		slots: map[ast.Node]int{}, builtins: map[string]int{}, consts: map[string]int{},
		temps: map[int]bool{}, free: map[string][]int{},
	}
	if owner != nil {
		f.f.Owner = owner.Name
	}
	for _, p := range params {
		f.slots[p.Node] = f.newSlot(p.Name, irType(p.Type))
	}
	f.f.Params = len(params)
	return f
}

func (f *fn) newSlot(name string, t *ir.Type) int {
	f.f.Slots = append(f.f.Slots, ir.Slot{Name: name, Type: t})
	return len(f.f.Slots) - 1
}

// local is the slot of a variable.
func (f *fn) local(decl ast.Node, name string, t *ir.Type) int {
	s := f.newSlot(name, t)
	f.slots[decl] = s
	return s
}

// temp is a temporary slot until the end of the current language statement.
func (f *fn) temp(t *ir.Type) int {
	key := t.String()
	var s int
	if list := f.free[key]; len(list) > 0 {
		s = list[len(list)-1]
		f.free[key] = list[:len(list)-1]
	} else {
		s = f.newSlot("", t)
		f.temps[s] = true
	}
	f.active = append(f.active, s)
	return s
}

// release frees the temporary slots taken after mark.
func (f *fn) release(mark int) {
	for _, s := range f.active[mark:] {
		key := f.f.Slots[s].Type.String()
		f.free[key] = append(f.free[key], s)
	}
	f.active = f.active[:mark]
}

func (f *fn) konst(c ir.Const) ir.Operand {
	key := c.Key()
	i, ok := f.consts[key]
	if !ok {
		i = len(f.f.Consts)
		f.f.Consts = append(f.f.Consts, c)
		f.consts[key] = i
	}
	return ir.ConstOp(i)
}

// constOf returns the constant of an operand.
func (f *fn) constOf(o ir.Operand) (ir.Const, bool) {
	if !o.IsConst() {
		return ir.Const{}, false
	}
	return f.f.Consts[o.Const()], true
}

// typeOfOperand returns the type of an operand.
func (f *fn) typeOfOperand(o ir.Operand) *ir.Type {
	if o.IsConst() {
		return f.f.Consts[o.Const()].Type
	}
	return f.f.Slots[o.Slot()].Type
}

func (f *fn) newBlock() int {
	f.f.Blocks = append(f.f.Blocks, &ir.Block{})
	return len(f.f.Blocks) - 1
}

func (f *fn) setBlock(b int) { f.cur = b }

func (f *fn) terminated() bool {
	in := f.f.Blocks[f.cur].Instrs
	return len(in) > 0 && in[len(in)-1].Op.Info().Term
}

// emit adds an instruction to the current block. Code after a terminator
// is unreachable: it goes into a new block that optimization removes.
func (f *fn) emit(in ir.Instr) {
	if f.terminated() {
		f.setBlock(f.newBlock())
	}
	if !in.Op.Info().Dst {
		in.Dst = ir.NoSlot
	}
	if in.Line == 0 {
		in.File, in.Line = f.file, f.line
	}
	b := f.f.Blocks[f.cur]
	b.Instrs = append(b.Instrs, in)
}

// value adds an instruction with a result into a temporary slot of type t.
func (f *fn) value(t *ir.Type, in ir.Instr) ir.Operand {
	in.Dst = f.temp(t)
	f.emit(in)
	return ir.SlotOp(in.Dst)
}

func (f *fn) jmp(b int) {
	if !f.terminated() {
		f.emit(ir.Instr{Op: ir.OpJmp, Targets: []int{b}})
	}
}

func (f *fn) br(cond ir.Operand, t, e int) {
	f.emit(ir.Instr{Op: ir.OpBr, Args: []ir.Operand{cond}, Targets: []int{t, e}})
}

// ret ends a path: before returning from a command or a reaction, the on
// reactions to the emitted events are called.
func (f *fn) ret(result ir.Operand) {
	if f.reacts {
		f.emit(ir.Instr{Op: ir.OpReact})
	}
	if result == ir.NoOperand {
		f.emit(ir.Instr{Op: ir.OpRet})
		return
	}
	f.emit(ir.Instr{Op: ir.OpRetVal, Args: []ir.Operand{result}})
}

// assign writes a value into a slot: if the value was just computed into a
// temporary slot by the last instruction of the block, that instruction
// writes straight into the target. Values gathered from several blocks
// (and, or, ??, ?.) arrive in a new empty block and are copied.
func (f *fn) assign(dst int, v ir.Operand) {
	if v.IsSlot() && v.Slot() == dst {
		return
	}
	if v.IsSlot() && f.temps[v.Slot()] {
		b := f.f.Blocks[f.cur]
		if n := len(b.Instrs); n > 0 && b.Instrs[n-1].Dst == v.Slot() {
			b.Instrs[n-1].Dst = dst
			return
		}
	}
	f.emit(ir.Instr{Op: ir.OpMove, Dst: dst, Args: []ir.Operand{v}})
}

// finish ends the function: the last path returns.
func (f *fn) finish() {
	if !f.terminated() {
		f.ret(ir.NoOperand)
	}
	for _, b := range f.f.Blocks {
		if n := len(b.Instrs); n == 0 || !b.Instrs[n-1].Op.Info().Term {
			b.Instrs = append(b.Instrs, ir.Instr{Op: ir.OpRet, Dst: ir.NoSlot})
		}
	}
}

// Built-in values that do not change during a function: evaluated once at
// the start.
var cachedBuiltins = []struct {
	name string
	op   ir.Op
}{{"id", ir.OpID}, {"now", ir.OpNow}, {"today", ir.OpToday}, {"me", ir.OpMe}, {"caller", ir.OpCaller}}

// prologue evaluates the built-in values that occur in the nodes.
func (f *fn) prologue(nodes ...ast.Node) {
	used := map[string]types.Type{}
	for _, n := range nodes {
		if n == nil {
			continue
		}
		ast.Inspect(n, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if o := f.l.info.Uses[x]; o != nil && o.Kind == sema.ObjBuiltin {
					used[o.Name] = o.Type
				}
			case *ast.CallExpr:
				// A call of its own command is addressed by id.
				if c := f.l.info.Calls[x]; c != nil && c.Kind == sema.CallMethod && c.Method.Kind == types.Command {
					used["id"] = c.Decl.ID
				}
			}
			return true
		})
	}
	for _, b := range cachedBuiltins {
		t, ok := used[b.name]
		if !ok || types.IsInvalid(t) {
			continue
		}
		s := f.newSlot(b.name, irType(t))
		f.builtins[b.name] = s
		f.emit(ir.Instr{Op: b.op, Dst: s})
	}
}
