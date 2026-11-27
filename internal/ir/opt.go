package ir

// Optimize performs minimal optimizations of a function (plan, §13.5): a
// branch on a constant becomes a jump, jumps through empty blocks are
// shortened, unreachable blocks are removed, unused slots and constants are
// dropped. Lowering folds constant expressions.
func Optimize(f *Func) {
	foldBranches(f)
	threadJumps(f)
	removeUnreachable(f)
	compactConsts(f)
	compactSlots(f)
}

func foldBranches(f *Func) {
	for _, b := range f.Blocks {
		last := &b.Instrs[len(b.Instrs)-1]
		if last.Op != OpBr || !last.Args[0].IsConst() {
			continue
		}
		c := f.Consts[last.Args[0].Const()]
		target := last.Targets[1]
		if c.Text == "true" {
			target = last.Targets[0]
		}
		*last = Instr{Op: OpJmp, Dst: NoSlot, Targets: []int{target}}
	}
}

// threadJumps routes jumps past blocks that hold only a jmp.
func threadJumps(f *Func) {
	final := func(t int) int {
		seen := map[int]bool{}
		for !seen[t] {
			seen[t] = true
			in := f.Blocks[t].Instrs
			if len(in) != 1 || in[0].Op != OpJmp {
				break
			}
			t = in[0].Targets[0]
		}
		return t
	}
	for _, b := range f.Blocks {
		last := &b.Instrs[len(b.Instrs)-1]
		for j, t := range last.Targets {
			last.Targets[j] = final(t)
		}
	}
}

// removeUnreachable removes unreachable blocks and orders the rest in
// reverse postorder: the dump reads top to bottom, the "true" branch comes
// before the "false" one, a loop body before the exit.
func removeUnreachable(f *Func) {
	seen := make([]bool, len(f.Blocks))
	var post []int
	var visit func(i int)
	visit = func(i int) {
		seen[i] = true
		last := f.Blocks[i].Instrs[len(f.Blocks[i].Instrs)-1]
		for j := len(last.Targets) - 1; j >= 0; j-- {
			if t := last.Targets[j]; !seen[t] {
				visit(t)
			}
		}
		post = append(post, i)
	}
	visit(0)
	newIndex := make([]int, len(f.Blocks))
	kept := make([]*Block, len(post))
	for i, b := range post {
		j := len(post) - 1 - i
		newIndex[b] = j
		kept[j] = f.Blocks[b]
	}
	for _, b := range kept {
		last := &b.Instrs[len(b.Instrs)-1]
		for j, t := range last.Targets {
			last.Targets[j] = newIndex[t]
		}
	}
	f.Blocks = kept
}

// eachOperand calls fn for every operand of an instruction, the search
// template operands included.
func eachOperand(in *Instr, fn func(o *Operand)) {
	for i := range in.Args {
		fn(&in.Args[i])
	}
	if s := in.Find; s != nil {
		for i := range s.Where {
			fn(&s.Where[i].Value)
		}
		fn(&s.Page)
		fn(&s.Limit)
	}
}

// compactConsts keeps the used constants in the pool in order of first use.
func compactConsts(f *Func) {
	remap := map[int]int{}
	var pool []Const
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			eachOperand(&b.Instrs[i], func(o *Operand) {
				if !o.IsConst() {
					return
				}
				j, ok := remap[o.Const()]
				if !ok {
					j = len(pool)
					remap[o.Const()] = j
					pool = append(pool, f.Consts[o.Const()])
				}
				*o = ConstOp(j)
			})
		}
	}
	f.Consts = pool
}

// compactSlots removes slots that do not occur in instructions: parameters
// stay first, the rest are numbered by first occurrence.
func compactSlots(f *Func) {
	remap := map[int]int{}
	slots := append([]Slot(nil), f.Slots[:f.Params]...)
	for i := 0; i < f.Params; i++ {
		remap[i] = i
	}
	use := func(s int) int {
		j, ok := remap[s]
		if !ok {
			j = len(slots)
			remap[s] = j
			slots = append(slots, f.Slots[s])
		}
		return j
	}
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			eachOperand(in, func(o *Operand) {
				if o.IsSlot() {
					*o = SlotOp(use(o.Slot()))
				}
			})
			if in.Dst != NoSlot {
				in.Dst = use(in.Dst)
			}
		}
	}
	f.Slots = slots
}
