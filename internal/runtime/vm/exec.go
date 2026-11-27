package vm

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// exec runs function f whose registers start at base on the stack. On
// return it removes the events the function emitted from the reaction list
// of call: the react instruction has already called the reactions to them.
func (r *run) exec(f *bytecode.Func, code []instr, base int, fr *frame) (model.Value, error) {
	n := f.Frame()
	regs := r.stack[base : base+n : base+n]
	a := r.a
	self := fr.self
	emitStart := len(r.emitted)
	refresh := func() { regs = r.stack[base : base+n : base+n] }
	for pc := 0; ; {
		in := &code[pc]
		next := pc + int(in.Size)
		var err error
		switch in.Op {
		case bytecode.OpMove:
			regs[in.A] = regs[in.B]
		case bytecode.OpField:
			if self.Kind != model.KindRecord || in.B >= self.Ref.Len {
				err = kindError(in.Op, self)
				break
			}
			regs[in.A] = a.Elems(self)[in.B]
		case bytecode.OpRecGet, bytecode.OpValGet:
			x := regs[in.B]
			if x.Kind != model.KindRecord || int(in.C) >= int(x.Ref.Len) {
				err = fmt.Errorf("%w: field %d of a %s value", ErrUnsupported, in.C, x.Kind)
				break
			}
			regs[in.A] = a.Elems(x)[in.C]
		case bytecode.OpRecID:
			x := regs[in.B]
			if x.Kind != model.KindRecord || x.Ref.Len == 0 {
				err = fmt.Errorf("%w: identifier of a %s value", ErrUnsupported, x.Kind)
				break
			}
			e := a.Elems(x)
			regs[in.A] = e[len(e)-1]
		case bytecode.OpValNew:
			s := &f.Sites[in.B]
			m := a.Mark()
			for _, arg := range s.Args {
				a.Push(regs[arg])
			}
			v := a.End(model.KindRecord, s.Type, m)
			if s.Func != bytecode.NoFunc {
				var ok model.Value
				if ok, err = r.call(s.Func, nil, frame{self: v}); err != nil {
					break
				}
				refresh()
				if !ok.Bool() {
					err = fmt.Errorf("%w %s", ErrCheck, r.m.schema.Type(s.Type).Name)
					break
				}
			}
			regs[in.A] = v
		case bytecode.OpCollList, bytecode.OpCollSet, bytecode.OpCollMap:
			regs[in.A] = r.collection(in.Op, f.Sites[in.B].Args, regs)
		case bytecode.OpLen:
			x := regs[in.B]
			if !isColl(x) && x.Kind != model.KindString && x.Kind != model.KindBytes {
				err = kindError(in.Op, x)
				break
			}
			regs[in.A] = model.Int(int64(a.Len(x)))
		case bytecode.OpGet:
			xs, i := regs[in.B], regs[in.C].Int()
			if !isColl(xs) {
				err = kindError(in.Op, xs)
				break
			}
			step := int64(1)
			if xs.Kind == model.KindMap {
				step = 2
			}
			if i < 0 || i*step >= int64(xs.Ref.Len) {
				err = fmt.Errorf("%w: index %d outside the collection", ErrUnsupported, i)
				break
			}
			regs[in.A] = a.Elems(xs)[i*step]
		case bytecode.OpAppend:
			if regs[in.B].Kind != model.KindList {
				err = kindError(in.Op, regs[in.B])
				break
			}
			regs[in.A] = r.appendList(regs[in.B], regs[in.C])
		case bytecode.OpPageWith:
			if x := regs[in.B]; x.Kind != model.KindPage || x.Ref.Len != 2 || regs[in.C].Kind != model.KindList {
				err = kindError(in.Op, x)
				break
			}
			p := a.Elems(regs[in.B])
			m := a.Mark()
			a.Push(regs[in.C])
			a.Push(p[1])
			regs[in.A] = a.End(model.KindPage, 0, m)
		case bytecode.OpInList, bytecode.OpInSet, bytecode.OpInMap:
			if !isColl(regs[in.C]) {
				err = kindError(in.Op, regs[in.C])
				break
			}
			step := 1
			if in.Op == bytecode.OpInMap {
				step = 2
			}
			regs[in.A] = model.Bool(indexIn(a, regs[in.C], regs[in.B], step) >= 0)
		case bytecode.OpIsNone:
			regs[in.A] = model.Bool(regs[in.B].Kind == model.KindNone)
		case bytecode.OpNot:
			regs[in.A] = model.Bool(!regs[in.B].Bool())
		case bytecode.OpIDConv:
			v := regs[in.B]
			v.Type = model.TypeRef(in.C)
			regs[in.A] = v
		case bytecode.OpJmp:
			next = int(in.B)
		case bytecode.OpBrTrue:
			if regs[in.A].Lo != 0 {
				next = int(in.B)
			}
		case bytecode.OpBrFalse:
			if regs[in.A].Lo == 0 {
				next = int(in.B)
			}
		case bytecode.OpRet:
			return model.None, nil
		case bytecode.OpRetVal:
			return regs[in.A], nil
		case bytecode.OpLoopGuard:
			// Iterations count backward jumps (below): so the limit applies
			// to programs without loop.guard too.
		case bytecode.OpError:
			s := &f.Sites[in.B]
			r.errors = append(r.errors, model.BusinessError{
				Owner: s.Type, Name: r.m.schema.Type(s.Type).Methods[s.Method].Name, Args: r.argsRecord(s.Args, regs),
				Subjects: r.argSubjects(s, fr),
			})
		case bytecode.OpRecLoad:
			if info := r.m.inner[model.TypeRef(in.C)]; info != nil {
				regs[in.A] = r.innerRecord(fr, info, model.TypeRef(in.C), regs[in.B])
			} else {
				regs[in.A], err = r.load(model.TypeRef(in.C), regs[in.B])
			}
		case bytecode.OpInnerAll:
			if info := r.m.inner[model.TypeRef(in.C)]; info != nil {
				regs[in.A] = r.innerAll(fr, info, model.TypeRef(in.C))
			} else {
				err = fmt.Errorf("%w: %d is not an inner entity", ErrUnsupported, in.C)
			}
		case bytecode.OpFind:
			regs[in.A], err = r.find(&f.Finds[in.B], regs)
		case bytecode.OpQueryCall, bytecode.OpRuleCall, bytecode.OpMethod:
			s := &f.Sites[in.B]
			callee, args := frame{}, s.Args
			switch in.Op {
			case bytecode.OpMethod:
				callee.self, args = regs[args[0]], args[1:]
			case bytecode.OpRuleCall:
				callee = *fr // an entity rule reads the snapshot fields of the calling function
			}
			vals := r.args[:0]
			for _, arg := range args {
				vals = append(vals, regs[arg])
			}
			r.args = vals
			var res model.Value
			if res, err = r.call(s.Func, vals, callee); err != nil {
				break
			}
			refresh()
			regs[in.A] = res
		case bytecode.OpIntrinsic:
			s := &f.Sites[in.B]
			regs[in.A], err = r.intrinsic(s.Func, s.Args, regs)
		case bytecode.OpNow:
			regs[in.A] = r.now
		case bytecode.OpToday:
			regs[in.A] = model.DateDays(floorDiv(int64(r.now.Lo), 86400) + epochDays)
		case bytecode.OpHasErrors:
			regs[in.A] = model.Bool(len(r.errors) > 0)
		case bytecode.OpMe:
			regs[in.A] = r.subject
		case bytecode.OpCaller:
			regs[in.A] = r.callerValue()
		case bytecode.OpFeature:
			regs[in.A] = model.Bool(r.caller.Features.Has(int(in.B)))
		case bytecode.OpSelf:
			regs[in.A] = self
		case bytecode.OpAfterField, bytecode.OpAfter, bytecode.OpID:
			if fr.ent == nil {
				err = ErrUnsupported
				break
			}
			switch in.Op {
			case bytecode.OpAfterField:
				if in.B >= fr.ent.state.Ref.Len {
					err = kindError(in.Op, fr.ent.state)
					break
				}
				regs[in.A] = a.Elems(fr.ent.state)[in.B]
			case bytecode.OpID:
				regs[in.A] = fr.ent.id
			default:
				m := a.Mark()
				for _, v := range a.Elems(fr.ent.state) {
					a.Push(v)
				}
				a.Push(fr.ent.id)
				regs[in.A] = a.End(model.KindRecord, fr.ent.t, m)
			}
		case bytecode.OpSet:
			if self.Kind != model.KindRecord || in.A >= self.Ref.Len || f.Kind != bytecode.FuncApply {
				err = kindError(in.Op, self) // set is only in apply over the new state
				break
			}
			a.Elems(self)[in.A] = regs[in.B]
		case bytecode.OpEvent:
			err = r.event(fr, &f.Sites[in.B], regs)
			refresh()
		case bytecode.OpReact:
			err = r.react(emitStart)
			refresh()
		case bytecode.OpCmd, bytecode.OpCmdOpt:
			if r.migrate != nil {
				err = r.migrationUnit(&f.Sites[in.B], regs, in.Op == bytecode.OpCmdOpt)
			} else {
				err = r.cmd(fr, &f.Sites[in.B], regs, in.Op == bytecode.OpCmdOpt)
			}
			refresh()
		case bytecode.OpNotify:
			err = r.notify(&f.Sites[in.B], regs, fr)
		case bytecode.OpUpsert:
			r.upsert(&f.Sites[in.B], regs)
		case bytecode.OpProjDelete:
			r.projections = append(r.projections, model.ProjectionOp{
				Kind: model.ProjectionDelete, Projection: model.TypeRef(in.C), ID: regs[in.A], Seq: r.seq,
			})
			r.seq++
		case bytecode.OpSchedule:
			regs[in.A], err = r.schedule(fr, &f.Sites[in.B], regs)
		case bytecode.OpTimerDelete:
			if id := regs[in.A]; id.Kind != model.KindNone {
				r.timers = append(r.timers, model.TimerOp{Kind: model.TimerDelete, ID: id})
			}
		case bytecode.OpRequest:
			err = r.request(&f.Sites[in.B], regs, fr)
		case bytecode.OpCallOut:
			err = r.callOut(&f.Sites[in.B], regs, fr)
		case bytecode.OpForget:
			r.forgetSubject(model.TypeRef(in.C), regs[in.A])
		case bytecode.OpRefExists, bytecode.OpRefLoad:
			var ok bool
			ok, err = r.exists(fr, model.TypeRef(in.C), regs[in.B])
			switch {
			case in.Op == bytecode.OpRefExists:
				regs[in.A] = model.Bool(ok)
			case ok:
				regs[in.A] = regs[in.B] // a reference [x] is the identifier itself
			default:
				regs[in.A] = model.None
			}
		case bytecode.OpAll:
			regs[in.A], err = r.all(model.TypeRef(in.C))
		case bytecode.OpTestCmd, bytecode.OpTestQuery, bytecode.OpTestTime, bytecode.OpTestWhen, bytecode.OpTestFeature,
			bytecode.OpExpectNotify, bytecode.OpExpectError, bytecode.OpExpectReq, bytecode.OpExpectMatch:
			if r.test == nil {
				err = ErrUnsupported
				break
			}
			r.test.pc = pc
			if r.test.phase == PhaseWhen && r.test.section == Given && in.Op != bytecode.OpTestWhen {
				// The when path: the PhaseGiven run already executed the given steps.
				if in.Op == bytecode.OpTestQuery {
					regs[in.A] = model.None
				}
				break
			}
			if r.test.phase == PhaseGiven && in.Op == bytecode.OpTestWhen {
				return model.None, errStop // given is done
			}
			switch in.Op {
			case bytecode.OpTestCmd:
				err = r.testCmd(&f.Sites[in.B], regs)
			case bytecode.OpTestQuery:
				regs[in.A], err = r.testQuery(&f.Sites[in.B], regs)
			case bytecode.OpTestTime:
				err = r.testTime(regs[in.A])
			case bytecode.OpTestWhen:
				r.test.section, r.test.steps = When, 0
			case bytecode.OpTestFeature:
				err = r.testFeature(int(in.B))
			case bytecode.OpExpectNotify:
				r.expectNotify(&f.Sites[in.B], regs)
			case bytecode.OpExpectError:
				r.expectError(&f.Sites[in.B], regs)
			case bytecode.OpExpectReq:
				r.expectRequest(&f.Sites[in.B], regs)
			case bytecode.OpExpectMatch:
				r.expectMatch(&f.Sites[in.B], regs)
			}
			if errors.Is(err, errStop) {
				return model.None, err
			}
		case bytecode.OpNewID:
			var id model.Value
			if info := r.m.inner[model.TypeRef(in.C)]; info != nil && r.m.schema.Type(model.TypeRef(in.C)).ID == model.IDSequence {
				id = r.innerNewID(fr, info, model.TypeRef(in.C))
			} else if id, err = r.env.NewID(r.ctx, a, model.TypeRef(in.C)); err != nil {
				err = fmt.Errorf("Env.NewID: %w", err)
			}
			regs[in.A] = id
		default:
			if int(in.Op) >= bytecode.NumFixedOps() {
				regs[in.A], err = typedOps[in.Op](r, regs[in.B], regs[in.C])
			} else {
				err = fmt.Errorf("%w: %s", ErrUnsupported, in.Op)
			}
		}
		if next <= pc && err == nil {
			// A backward jump is a loop iteration: the limit applies to code
			// without loop.guard too (a program not from the compiler).
			if r.iters++; r.iters > r.m.limits.Iterations {
				err = fmt.Errorf("%w: more than %d loop iterations", ErrLimit, r.m.limits.Iterations)
			} else if b, v := a.Size(); r.m.limits.Memory > 0 && b+v*valueSize > r.m.limits.Memory {
				err = fmt.Errorf("%w: the unit of work arena exceeds %d bytes", ErrLimit, r.m.limits.Memory)
			}
		}
		if err != nil {
			if flt, ok := err.(*Fault); ok {
				// A failure in a nested call: this function is the next stack frame.
				file, line := r.m.prog.Position(f, pc)
				flt.Stack = append(flt.Stack, Frame{Func: f.Name, PC: pc, File: file, Line: line})
				return model.None, flt
			}
			return model.None, r.fault(f, pc, in.Op, err)
		}
		pc = next
	}
}

// epochDays is the number of days from 0001-01-01 to 1970-01-01.
const epochDays = 719162

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// argsRecord builds an arguments record (type 0) from registers.
func (r *run) argsRecord(args []uint32, regs []model.Value) model.Record {
	m := r.a.Mark()
	for _, arg := range args {
		r.a.Push(regs[arg])
	}
	return r.a.End(model.KindRecord, 0, m).Record()
}

// callerValue is caller as a value: a record without a type with one field, roles.
func (r *run) callerValue() model.Value {
	if r.callerV.Kind == model.KindNone {
		m := r.a.Mark()
		for _, role := range r.caller.Roles {
			r.a.Push(r.a.String(role))
		}
		roles := r.a.End(model.KindSet, 0, m)
		r.callerV = r.a.Record(0, roles).Value()
	}
	return r.callerV
}

// isColl reports that a value is a list, set or map.
func isColl(v model.Value) bool {
	return v.Kind == model.KindList || v.Kind == model.KindSet || v.Kind == model.KindMap
}

// kindError is a failure: an operand of the wrong kind (a program not from the compiler).
func kindError(op bytecode.Op, v model.Value) error {
	return fmt.Errorf("%w: %s on a %s value", ErrUnsupported, op, v.Kind)
}

// valueSize is the size of a model.Value in the arena, in bytes.
const valueSize = int(unsafe.Sizeof(model.Value{}))
