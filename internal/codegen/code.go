package codegen

import (
	"fmt"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// fgen builds the code of one function.
type fgen struct {
	g      *gen
	f      *ir.Func
	out    *bytecode.Func
	starts []int   // block start addresses
	fixups []fixup // jumps whose address is known after layout
}

type fixup struct {
	pc    int // the instruction word (not the prefix)
	block int
}

// line adds a position table entry if the position of instruction in
// differs from the previous entry.
func (fg *fgen) line(in ir.Instr) {
	if in.Line == 0 {
		return
	}
	l := bytecode.Line{PC: uint32(len(fg.out.Code)), File: uint32(in.File), Line: uint32(in.Line)}
	lines := fg.out.Lines
	if n := len(lines); n > 0 {
		if lines[n-1].File == l.File && lines[n-1].Line == l.Line {
			return
		}
		if lines[n-1].PC == l.PC {
			lines[n-1] = l
			return
		}
	}
	fg.out.Lines = append(lines, l)
}

// code lays blocks out in order: a jump to the next block is omitted, and br
// becomes br.true or br.false with a fall-through.
func (fg *fgen) code() error {
	fg.starts = make([]int, len(fg.f.Blocks))
	if fg.out.Frame() > 0xffff {
		return fmt.Errorf("%d registers exceed 65535", fg.out.Frame())
	}
	for bi, b := range fg.f.Blocks {
		fg.starts[bi] = len(fg.out.Code)
		for _, in := range b.Instrs {
			fg.line(in)
			var err error
			switch in.Op {
			case ir.OpJmp:
				if in.Targets[0] != bi+1 {
					err = fg.jump(bytecode.OpJmp, 0, in.Targets[0])
				}
			case ir.OpBr:
				c, t, f := fg.reg(in.Args[0]), in.Targets[0], in.Targets[1]
				switch {
				case f == bi+1:
					err = fg.jump(bytecode.OpBrTrue, c, t)
				case t == bi+1:
					err = fg.jump(bytecode.OpBrFalse, c, f)
				default:
					if err = fg.jump(bytecode.OpBrTrue, c, t); err == nil {
						err = fg.jump(bytecode.OpJmp, 0, f)
					}
				}
			default:
				err = fg.instr(in)
			}
			if err != nil {
				return fmt.Errorf("b%d: %s: %w", bi, in.Op, err)
			}
		}
	}
	if len(fg.out.Code) > 0xffff {
		return fmt.Errorf("function code is longer than 65535 words")
	}
	for _, fx := range fg.fixups {
		fg.out.Code[fx.pc] = fg.out.Code[fx.pc]&0xffff | uint32(fg.starts[fx.block])<<16
	}
	return nil
}

func (fg *fgen) emit(op bytecode.Op, a, b, c uint32) error {
	code, ok := bytecode.AppendInstr(fg.out.Code, op, a, b, c)
	if !ok {
		return fmt.Errorf("operands %d, %d, %d do not fit into instruction %s", a, b, c, op)
	}
	fg.out.Code = code
	return nil
}

func (fg *fgen) jump(op bytecode.Op, cond uint32, block int) error {
	if err := fg.emit(op, cond, 0, 0); err != nil {
		return err
	}
	fg.fixups = append(fg.fixups, fixup{pc: len(fg.out.Code) - 1, block: block})
	return nil
}

// reg returns the register of an operand: a slot or a constant.
func (fg *fgen) reg(o ir.Operand) uint32 {
	switch {
	case o == ir.NoOperand:
		return bytecode.NoReg
	case o.IsConst():
		return uint32(len(fg.f.Slots) + o.Const())
	}
	return uint32(o.Slot())
}

func (fg *fgen) regs(ops []ir.Operand) []uint32 {
	out := make([]uint32, len(ops))
	for i, o := range ops {
		out[i] = fg.reg(o)
	}
	return out
}

// opType returns the type of an IR operand (without "?").
func (fg *fgen) opType(o ir.Operand) *ir.Type {
	if o.IsConst() {
		return fg.f.Consts[o.Const()].Type.Unwrap()
	}
	return fg.f.Slots[o.Slot()].Type.Unwrap()
}

// site adds a call table entry and returns its number.
func (fg *fgen) site(s bytecode.Site) uint32 {
	fg.out.Sites = append(fg.out.Sites, s)
	return uint32(len(fg.out.Sites) - 1)
}

// fields converts the names of passed arguments into numbers; nil means all
// were passed in order.
func fields(names []string, all int, resolve func(string) (model.FieldRef, error)) ([]model.FieldRef, error) {
	out := make([]model.FieldRef, len(names))
	full := len(names) == all
	for i, n := range names {
		r, err := resolve(n)
		if err != nil {
			return nil, err
		}
		out[i] = r
		full = full && r == model.FieldRef(i)
	}
	if full {
		return nil, nil
	}
	return out, nil
}

// methodSite is the call entry of a method "Type.method": the arguments are
// the instruction operands, the names come after fixed fixed operands. fn
// is the kind of the called function, or -1.
func (fg *fgen) methodSite(in ir.Instr, fixed int, fn int) (bytecode.Site, error) {
	t, m, err := fg.g.method(in.Sym)
	if err != nil {
		return bytecode.Site{}, err
	}
	if len(in.Names) != len(in.Args)-fixed {
		return bytecode.Site{}, fmt.Errorf("%d names, %d arguments", len(in.Names), len(in.Args)-fixed)
	}
	s := bytecode.Site{Type: t, Method: m, Func: bytecode.NoFunc, Args: fg.regs(in.Args), Role: in.Aux}
	if fn >= 0 {
		fi, ok := fg.g.funcs[funcKey(ir.FuncKind(fn), in.Sym)]
		switch {
		case ok:
			s.Func = fi
		case ir.FuncKind(fn) != ir.FuncApply: // an event without a body has no apply function
			return s, fmt.Errorf("no function %s %s", ir.FuncKind(fn), in.Sym)
		}
	}
	params := len(fg.g.schema.Type(t).Methods[m].Params)
	s.Fields, err = fields(in.Names, params, func(n string) (model.FieldRef, error) { return fg.g.param(t, m, n) })
	return s, err
}

func (fg *fgen) instr(in ir.Instr) error {
	g := fg.g
	dst := uint32(0)
	if in.Op.Info().Dst {
		dst = uint32(in.Dst)
	}
	arg := func(i int) uint32 { return fg.reg(in.Args[i]) }
	withSite := func(op bytecode.Op, s bytecode.Site, err error) error {
		if err != nil {
			return err
		}
		return fg.emit(op, dst, fg.site(s), 0)
	}
	typeOf := func() (uint32, error) {
		t, err := g.typeRef(in.Sym)
		return uint32(t), err
	}
	switch in.Op {
	case ir.OpMove:
		return fg.emit(bytecode.OpMove, dst, arg(0), 0)
	case ir.OpField, ir.OpAfterField, ir.OpSetField:
		fr, err := g.field(fg.out.Owner, in.Sym)
		if err != nil {
			return err
		}
		switch in.Op {
		case ir.OpField:
			return fg.emit(bytecode.OpField, dst, uint32(fr), 0)
		case ir.OpAfterField:
			return fg.emit(bytecode.OpAfterField, dst, uint32(fr), 0)
		}
		return fg.emit(bytecode.OpSet, uint32(fr), arg(0), 0)
	case ir.OpAfter:
		return fg.emit(bytecode.OpAfter, dst, 0, 0)
	case ir.OpRecGet, ir.OpValGet:
		if in.Op == ir.OpRecGet && in.Sym == "id" {
			return fg.emit(bytecode.OpRecID, dst, arg(0), 0)
		}
		t, err := g.typeRef(fg.opType(in.Args[0]).Name)
		if err != nil {
			return err
		}
		fr, err := g.field(t, in.Sym)
		if err != nil {
			return err
		}
		op := bytecode.OpRecGet
		if in.Op == ir.OpValGet {
			op = bytecode.OpValGet
		}
		return fg.emit(op, dst, arg(0), uint32(fr))
	case ir.OpValNew:
		t, err := g.typeRef(in.Sym)
		if err != nil {
			return err
		}
		ti := g.schema.Type(t)
		if len(in.Names) != len(ti.Fields) {
			return fmt.Errorf("%s: %d fields, %d in the schema", in.Sym, len(in.Names), len(ti.Fields))
		}
		for i, n := range in.Names {
			if ti.Fields[i].Name != n {
				return fmt.Errorf("%s: field %d is %s, %s in the schema", in.Sym, i, n, ti.Fields[i].Name)
			}
		}
		check := bytecode.NoFunc
		if fi, ok := g.funcs[funcKey(ir.FuncCheck, in.Sym+".check")]; ok {
			check = fi
		}
		return withSite(bytecode.OpValNew, bytecode.Site{Type: t, Method: bytecode.NoMethod, Func: check, Args: fg.regs(in.Args)}, nil)
	case ir.OpCollList, ir.OpCollSet, ir.OpCollMap:
		op := map[ir.Op]bytecode.Op{ir.OpCollList: bytecode.OpCollList, ir.OpCollSet: bytecode.OpCollSet, ir.OpCollMap: bytecode.OpCollMap}[in.Op]
		return withSite(op, bytecode.Site{Method: bytecode.NoMethod, Func: bytecode.NoFunc, Args: fg.regs(in.Args)}, nil)
	case ir.OpLen, ir.OpIsNone, ir.OpNot, ir.OpRetVal, ir.OpTimerDelete, ir.OpTestTime:
		op := map[ir.Op]bytecode.Op{ir.OpLen: bytecode.OpLen, ir.OpIsNone: bytecode.OpIsNone, ir.OpNot: bytecode.OpNot,
			ir.OpRetVal: bytecode.OpRetVal, ir.OpTimerDelete: bytecode.OpTimerDelete, ir.OpTestTime: bytecode.OpTestTime}[in.Op]
		if !in.Op.Info().Dst {
			return fg.emit(op, arg(0), 0, 0)
		}
		return fg.emit(op, dst, arg(0), 0)
	case ir.OpGet, ir.OpAppend, ir.OpPageWith, ir.OpInList, ir.OpInSet, ir.OpInMap:
		op := map[ir.Op]bytecode.Op{ir.OpGet: bytecode.OpGet, ir.OpAppend: bytecode.OpAppend, ir.OpPageWith: bytecode.OpPageWith,
			ir.OpInList: bytecode.OpInList, ir.OpInSet: bytecode.OpInSet, ir.OpInMap: bytecode.OpInMap}[in.Op]
		return fg.emit(op, dst, arg(0), arg(1))
	case ir.OpIDConv, ir.OpRefExists, ir.OpRefLoad, ir.OpRecLoad:
		t, err := typeOf()
		if err != nil {
			return err
		}
		op := map[ir.Op]bytecode.Op{ir.OpIDConv: bytecode.OpIDConv, ir.OpRefExists: bytecode.OpRefExists,
			ir.OpRefLoad: bytecode.OpRefLoad, ir.OpRecLoad: bytecode.OpRecLoad}[in.Op]
		return fg.emit(op, dst, arg(0), t)
	case ir.OpProjDelete, ir.OpForget:
		t, err := typeOf()
		if err != nil {
			return err
		}
		op := bytecode.OpProjDelete
		if in.Op == ir.OpForget {
			op = bytecode.OpForget
		}
		return fg.emit(op, arg(0), 0, t)
	case ir.OpAll, ir.OpInnerAll, ir.OpNewID:
		t, err := typeOf()
		if err != nil {
			return err
		}
		op := map[ir.Op]bytecode.Op{ir.OpAll: bytecode.OpAll, ir.OpInnerAll: bytecode.OpInnerAll, ir.OpNewID: bytecode.OpNewID}[in.Op]
		return fg.emit(op, dst, 0, t)
	case ir.OpRet, ir.OpLoopGuard, ir.OpReact, ir.OpTestWhen:
		op := map[ir.Op]bytecode.Op{ir.OpRet: bytecode.OpRet, ir.OpLoopGuard: bytecode.OpLoopGuard,
			ir.OpReact: bytecode.OpReact, ir.OpTestWhen: bytecode.OpTestWhen}[in.Op]
		return fg.emit(op, 0, 0, 0)
	case ir.OpFeature, ir.OpTestFeature:
		i := fg.g.feature(in.Sym)
		if i < 0 {
			return fmt.Errorf("no feature flag %s in the schema", in.Sym)
		}
		if in.Op == ir.OpTestFeature {
			return fg.emit(bytecode.OpTestFeature, 0, uint32(i), 0)
		}
		return fg.emit(bytecode.OpFeature, dst, uint32(i), 0)
	case ir.OpNow, ir.OpToday, ir.OpID, ir.OpHasErrors, ir.OpMe, ir.OpCaller, ir.OpSelf:
		op := map[ir.Op]bytecode.Op{ir.OpNow: bytecode.OpNow, ir.OpToday: bytecode.OpToday, ir.OpID: bytecode.OpID,
			ir.OpHasErrors: bytecode.OpHasErrors, ir.OpMe: bytecode.OpMe, ir.OpCaller: bytecode.OpCaller, ir.OpSelf: bytecode.OpSelf}[in.Op]
		return fg.emit(op, dst, 0, 0)
	case ir.OpEvent:
		s, err := fg.methodSite(in, 0, int(ir.FuncApply))
		return withSite(bytecode.OpEvent, s, err)
	case ir.OpNotify:
		s, err := fg.methodSite(in, 1, -1)
		if err == nil {
			s.Subjects, err = fg.subjects(s, in.Subjects)
		}
		return withSite(bytecode.OpNotify, s, err)
	case ir.OpError:
		s, err := fg.methodSite(in, 0, -1)
		if err == nil {
			s.Subjects, err = fg.subjects(s, in.Subjects)
		}
		return withSite(bytecode.OpError, s, err)
	case ir.OpCmd, ir.OpCmdOpt:
		s, err := fg.methodSite(in, 1, int(ir.FuncCommand))
		op := bytecode.OpCmd
		if in.Op == ir.OpCmdOpt {
			op = bytecode.OpCmdOpt
		}
		return withSite(op, s, err)
	case ir.OpCallOut:
		s, err := fg.methodSite(in, 1, int(ir.FuncCommand))
		return withSite(bytecode.OpCallOut, s, err)
	case ir.OpSchedule:
		s, err := fg.methodSite(in, 1, int(ir.FuncCommand))
		return withSite(bytecode.OpSchedule, s, err)
	case ir.OpRequest:
		s, err := fg.methodSite(in, 0, -1)
		if err == nil {
			s.Subjects, err = fg.subjects(s, in.Subjects)
		}
		return withSite(bytecode.OpRequest, s, err)
	case ir.OpQueryCall:
		s, err := fg.methodSite(in, 0, int(ir.FuncQuery))
		return withSite(bytecode.OpQueryCall, s, err)
	case ir.OpRuleCall:
		s, err := fg.methodSite(in, 0, int(ir.FuncRule))
		return withSite(bytecode.OpRuleCall, s, err)
	case ir.OpMethod:
		s, err := fg.methodSite(in, 1, int(ir.FuncMethod))
		return withSite(bytecode.OpMethod, s, err)
	case ir.OpTestCmd:
		s, err := fg.methodSite(in, 2, int(ir.FuncCommand))
		return withSite(bytecode.OpTestCmd, s, err)
	case ir.OpTestQuery:
		s, err := fg.methodSite(in, 1, int(ir.FuncQuery))
		return withSite(bytecode.OpTestQuery, s, err)
	case ir.OpExpectNotify:
		s, err := fg.methodSite(in, 1, -1)
		return withSite(bytecode.OpExpectNotify, s, err)
	case ir.OpExpectReq:
		s, err := fg.methodSite(in, 0, -1)
		return withSite(bytecode.OpExpectReq, s, err)
	case ir.OpExpectError:
		if !strings.Contains(in.Sym, ".") {
			s, err := fg.builtinError(in)
			return withSite(bytecode.OpExpectError, s, err)
		}
		s, err := fg.methodSite(in, 0, -1)
		return withSite(bytecode.OpExpectError, s, err)
	case ir.OpUpsert, ir.OpExpectMatch:
		t, err := g.typeRef(in.Sym)
		if err != nil {
			return err
		}
		all := len(g.schema.Type(t).Fields)
		resolve := func(n string) (model.FieldRef, error) { return g.field(t, n) }
		op := bytecode.OpExpectMatch
		if in.Op == ir.OpUpsert {
			op, all = bytecode.OpUpsert, -1 // upsert always lists the fields, id first
			resolve = func(n string) (model.FieldRef, error) {
				if n == "id" {
					return bytecode.IDField, nil
				}
				return g.field(t, n)
			}
		}
		fs, err := fields(in.Names, all, resolve)
		return withSite(op, bytecode.Site{Type: t, Method: bytecode.NoMethod, Func: bytecode.NoFunc, Args: fg.regs(in.Args), Fields: fs}, err)
	case ir.OpFind:
		i, err := fg.find(in.Find)
		if err != nil {
			return err
		}
		return fg.emit(bytecode.OpFind, dst, i, 0)
	case ir.OpIntrinsic:
		name := fg.intrinsicName(in)
		id := bytecode.IntrinsicIndex(name)
		if id == bytecode.NoFunc {
			return fmt.Errorf("no built-in function %s", name)
		}
		return withSite(bytecode.OpIntrinsic, bytecode.Site{Method: bytecode.NoMethod, Func: id, Args: fg.regs(in.Args)}, nil)
	}
	return fg.typed(in, dst)
}

func (fg *fgen) builtinError(in ir.Instr) (bytecode.Site, error) {
	params, ok := bytecode.BuiltinErrors[in.Sym]
	if !ok {
		return bytecode.Site{}, fmt.Errorf("no built-in error %s", in.Sym)
	}
	fs, err := fields(in.Names, len(params), func(n string) (model.FieldRef, error) {
		for i, p := range params {
			if p == n {
				return model.FieldRef(i), nil
			}
		}
		return 0, fmt.Errorf("%s has no parameter %s", in.Sym, n)
	})
	return bytecode.Site{Method: bytecode.NoMethod, Func: bytecode.NoFunc, Args: fg.regs(in.Args), Fields: fs, Name: in.Sym}, err
}

// typed lists typed operations. IR integer operations are the same for int
// and bigint (i64); int (32 bits) has its own i32 variants: arithmetic by
// the result type (with a 32-bit overflow check), comparisons by the
// operand type.
func (fg *fgen) typed(in ir.Instr, dst uint32) error {
	name := in.Op.String()
	if base, kind, ok := strings.Cut(name, "."); ok && kind == "i64" {
		switch base {
		case "add", "sub", "mul", "div", "rem", "neg":
			if fg.f.Slots[in.Dst].Type.Unwrap().Kind == ir.TInt {
				name = base + ".i32"
			}
		case "eq", "ne", "lt", "le", "gt", "ge":
			if fg.opType(in.Args[0]).Kind == ir.TInt {
				name = base + ".i32"
			}
		}
	}
	op, ok := bytecode.Lookup(name)
	if !ok || op < bytecode.Op(bytecode.NumFixedOps()) {
		return fmt.Errorf("no bytecode operation %s", name)
	}
	var b, c uint32
	if len(in.Args) > 0 {
		b = fg.reg(in.Args[0])
	}
	if len(in.Args) > 1 {
		c = fg.reg(in.Args[1])
	}
	return fg.emit(op, dst, b, c)
}

// suffixes are the value kind suffixes of built-in functions.
var suffixes = map[ir.TypeKind]string{ir.TInt: "i32", ir.TBigint: "i64", ir.TFloat: "f64", ir.TDecimal: "dec", ir.TString: "str"}

// intrinsicName is the name of a bytecode built-in function: numeric
// functions differ by the argument kind, sum by the result kind.
func (fg *fgen) intrinsicName(in ir.Instr) string {
	switch in.Sym {
	case "int", "bigint", "float", "decimal", "abs", "floor", "ceil", "min", "max", "round":
		if len(in.Args) > 0 {
			return in.Sym + "." + suffixes[fg.opType(in.Args[0]).Kind]
		}
	case "list.sum", "set.sum":
		return in.Sym + "." + suffixes[fg.f.Slots[in.Dst].Type.Unwrap().Kind]
	}
	return in.Sym
}

var condOps = map[ir.CondOp]model.CondOp{
	ir.CondEq: model.OpEq, ir.CondLt: model.OpLt, ir.CondLe: model.OpLe,
	ir.CondGt: model.OpGt, ir.CondGe: model.OpGe, ir.CondIn: model.OpIn,
}

// find adds a search template and returns its number.
func (fg *fgen) find(spec *ir.FindSpec) (uint32, error) {
	g := fg.g
	t, err := g.typeRef(spec.Type)
	if err != nil {
		return 0, err
	}
	out := bytecode.Find{Type: t, Index: -1, Page: fg.reg(spec.Page), Limit: fg.reg(spec.Limit)}
	if spec.Index != "" {
		ix := g.schema.Type(t).Index(spec.Index)
		if ix == nil {
			return 0, fmt.Errorf("%s has no index %s", spec.Type, spec.Index)
		}
		out.Index = int32(ix.Ref)
	}
	for _, c := range spec.Where {
		path, err := g.path(t, c.Path)
		if err != nil {
			return 0, err
		}
		op, ok := condOps[c.Op]
		if !ok {
			return 0, fmt.Errorf("unknown condition %s", c.Op)
		}
		out.Where = append(out.Where, bytecode.Cond{Field: path, Op: op, Value: fg.reg(c.Value)})
	}
	for _, k := range spec.Sort {
		path, err := g.path(t, k.Path)
		if err != nil {
			return 0, err
		}
		out.Sort = append(out.Sort, model.SortKey{Field: path, Desc: k.Desc})
	}
	fg.out.Finds = append(fg.out.Finds, out)
	return uint32(len(fg.out.Finds) - 1), nil
}

// subjects converts argument subjects into parameter and field numbers of
// the function's owner type.
func (fg *fgen) subjects(s bytecode.Site, ss []ir.Subject) ([]bytecode.SiteSubject, error) {
	if len(ss) == 0 {
		return nil, nil
	}
	owner, err := fg.g.typeRef(fg.f.Owner)
	if err != nil {
		return nil, err
	}
	out := make([]bytecode.SiteSubject, len(ss))
	for i, x := range ss {
		arg, err := fg.g.param(s.Type, s.Method, x.Arg)
		if err != nil {
			return nil, err
		}
		t, err := fg.g.typeRef(x.Type)
		if err != nil {
			return nil, err
		}
		path, err := fg.g.path(owner, x.Path)
		if err != nil {
			return nil, fmt.Errorf("subject %s of argument %s: %w", x.Type, x.Arg, err)
		}
		out[i] = bytecode.SiteSubject{Arg: int32(arg), Type: t, Path: path}
	}
	return out, nil
}

// path converts a path through fields (via value objects) into numbers; id
// is the record identifier.
func (g *gen) path(t model.TypeRef, names []string) (model.FieldPath, error) {
	var out model.FieldPath
	owner := t
	for i, n := range names {
		if n == "id" && i == 0 && g.schema.Type(owner).ID != model.IDNone {
			out = append(out, bytecode.IDField)
			owner = 0
			continue
		}
		if owner == 0 {
			return nil, fmt.Errorf("path %s: %s is not a value object", strings.Join(names, "."), names[i-1])
		}
		fr, err := g.field(owner, n)
		if err != nil {
			return nil, err
		}
		out = append(out, fr)
		ft := g.schema.Type(owner).Fields[fr].Type
		owner = 0
		if ft.Kind == model.KindRecord {
			owner = ft.Ref
		}
	}
	return out, nil
}
