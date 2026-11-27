package bytecode

import "fmt"

// assigned checks that every slot register an instruction reads is written
// on all paths from the function entry: parameters are written by the
// call, constants on entry. So the VM never reads an empty register instead
// of a value of its type. The analysis is a forward data flow over jumps:
// the set of written slots at an instruction's entry is the intersection
// over its predecessors.
func (v *verifier) assigned() error {
	f := v.f
	n := len(f.Regs)
	if n == 0 {
		return nil
	}
	words := (n + 63) / 64
	in := make([][]uint64, len(f.Code)) // nil: the instruction is not reached yet
	entry := make([]uint64, words)
	for r := 0; r < f.Params; r++ {
		entry[r/64] |= 1 << (r % 64)
	}
	in[0] = entry
	work := []int{0}
	out := make([]uint64, words)
	for len(work) > 0 {
		pc := work[len(work)-1]
		work = work[:len(work)-1]
		ins, _ := DecodeInstr(f.Code, pc)
		copy(out, in[pc])
		if d, ok := v.dst(ins); ok && int(d) < n {
			out[d/64] |= 1 << (d % 64)
		}
		for _, next := range successors(ins, pc) {
			if next >= len(f.Code) {
				continue
			}
			if in[next] == nil {
				in[next] = append([]uint64(nil), out...)
				work = append(work, next)
				continue
			}
			changed := false
			for i := range out {
				if x := in[next][i] & out[i]; x != in[next][i] {
					in[next][i], changed = x, true
				}
			}
			if changed {
				work = append(work, next)
			}
		}
	}
	for pc := 0; pc < len(f.Code); {
		ins, _ := DecodeInstr(f.Code, pc)
		if set := in[pc]; set != nil {
			for _, r := range v.reads(ins) {
				if int(r) < n && set[r/64]&(1<<(r%64)) == 0 {
					return fmt.Errorf("%d: %s reads r%d before it is written", pc, ins.Op, r)
				}
			}
		}
		pc += ins.Size
	}
	return nil
}

// successors are the addresses an instruction passes control to.
func successors(in Instr, pc int) []int {
	next := pc + in.Size
	switch in.Op {
	case OpRet, OpRetVal:
		return nil
	case OpJmp:
		return []int{int(in.B)}
	case OpBrTrue, OpBrFalse:
		return []int{next, int(in.B)}
	}
	return []int{next}
}

// dst is the result register of an instruction.
func (v *verifier) dst(in Instr) (uint32, bool) {
	vals := [3]uint32{in.A, in.B, in.C}
	for i, role := range in.Op.Info().Roles {
		if role == RoleDst {
			return vals[i], true
		}
	}
	return 0, false
}

// reads are the registers an instruction reads: operands, site arguments,
// condition values, the search page and limit.
func (v *verifier) reads(in Instr) []uint32 {
	var out []uint32
	vals := [3]uint32{in.A, in.B, in.C}
	for i, role := range in.Op.Info().Roles {
		switch role {
		case RoleReg:
			out = append(out, vals[i])
		case RoleRegField:
			out = append(out, in.B)
		case RoleSite:
			for _, a := range v.f.Sites[vals[i]].Args {
				if a != NoReg {
					out = append(out, a)
				}
			}
		case RoleFind:
			fd := &v.f.Finds[vals[i]]
			for _, c := range fd.Where {
				out = append(out, c.Value)
			}
			for _, r := range []uint32{fd.Page, fd.Limit} {
				if r != NoReg {
					out = append(out, r)
				}
			}
		}
	}
	return out
}
