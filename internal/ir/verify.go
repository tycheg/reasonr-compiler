package ir

import "fmt"

// Verify checks the integrity of a program: operations are known,
// instructions have the right number of operands and targets, slots,
// constants and blocks exist, every block ends with a terminator.
func Verify(p *Program) error {
	names := map[string]bool{}
	for _, f := range p.Funcs {
		key := f.Kind.String() + " " + f.Name
		if names[key] {
			return fmt.Errorf("repeated function %s", key)
		}
		names[key] = true
		if err := f.verify(); err != nil {
			return fmt.Errorf("%s %s: %w", f.Kind, f.Name, err)
		}
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				if in.File < 0 || in.File > len(p.Files) || in.Line < 0 || (in.File == 0) != (in.Line == 0) {
					return fmt.Errorf("%s %s: invalid position %d:%d", f.Kind, f.Name, in.File, in.Line)
				}
			}
		}
	}
	for _, r := range p.Reactions {
		for _, name := range r.Funcs {
			if p.Func(FuncReaction, name) == nil && p.Func(FuncACL, name) == nil {
				return fmt.Errorf("reaction to %s: no function %s", r.Event, name)
			}
		}
	}
	return nil
}

func (f *Func) verify() error {
	if f.Kind > FuncACL {
		return fmt.Errorf("unknown function kind %d", f.Kind)
	}
	if f.Params < 0 || f.Params > len(f.Slots) {
		return fmt.Errorf("%d parameters, %d slots", f.Params, len(f.Slots))
	}
	for i, s := range f.Slots {
		if err := verifyType(s.Type); err != nil {
			return fmt.Errorf("slot r%d: %w", i, err)
		}
	}
	if f.Result != nil {
		if err := verifyType(f.Result); err != nil {
			return fmt.Errorf("result: %w", err)
		}
	}
	for i, c := range f.Consts {
		if err := verifyType(c.Type); err != nil {
			return fmt.Errorf("constant %d: %w", i, err)
		}
	}
	if len(f.Blocks) == 0 {
		return fmt.Errorf("no blocks")
	}
	for bi, b := range f.Blocks {
		if len(b.Instrs) == 0 {
			return fmt.Errorf("b%d: empty block", bi)
		}
		for ii, in := range b.Instrs {
			if err := f.verifyInstr(in); err != nil {
				return fmt.Errorf("b%d[%d]: %w", bi, ii, err)
			}
			if term := in.Op.Info().Term; term != (ii == len(b.Instrs)-1) {
				if term {
					return fmt.Errorf("b%d[%d]: terminator %s is not at the end of the block", bi, ii, in.Op)
				}
				return fmt.Errorf("b%d: the block does not end with a terminator", bi)
			}
		}
	}
	return nil
}

// optionalFixed lists fixed operands that may be absent.
func optionalFixed(op Op, i int) bool {
	switch op {
	case OpNotify, OpExpectNotify, OpTestQuery:
		return i == 0
	case OpTestCmd:
		return i == 0 || i == 1 // entity: NoOperand is the fake instance
	}
	return false
}

func (f *Func) verifyInstr(in Instr) error {
	if !in.Op.Valid() {
		return fmt.Errorf("unknown operation %d", in.Op)
	}
	info := in.Op.Info()
	switch {
	case info.Dst && (in.Dst < 0 || in.Dst >= len(f.Slots)):
		return fmt.Errorf("%s: no result slot r%d", info.Name, in.Dst)
	case !info.Dst && in.Dst != NoSlot:
		return fmt.Errorf("%s: the operation has no result", info.Name)
	case len(in.Args) < info.Fixed:
		return fmt.Errorf("%s: %d operands, %d needed", info.Name, len(in.Args), info.Fixed)
	case !info.Variadic && !info.Named && len(in.Args) != info.Fixed:
		return fmt.Errorf("%s: %d operands, %d needed", info.Name, len(in.Args), info.Fixed)
	case info.Named && len(in.Names) != len(in.Args)-info.Fixed:
		return fmt.Errorf("%s: %d names, %d arguments", info.Name, len(in.Names), len(in.Args)-info.Fixed)
	case len(in.Targets) != info.Targets:
		return fmt.Errorf("%s: %d targets, %d needed", info.Name, len(in.Targets), info.Targets)
	case (in.Find != nil) != (in.Op == OpFind):
		return fmt.Errorf("%s: only find has a search template", info.Name)
	}
	for i, a := range in.Args {
		if a == NoOperand && i < info.Fixed && optionalFixed(in.Op, i) {
			continue
		}
		if err := f.verifyOperand(a); err != nil {
			return fmt.Errorf("%s: %w", info.Name, err)
		}
	}
	for _, t := range in.Targets {
		if t < 0 || t >= len(f.Blocks) {
			return fmt.Errorf("%s: no block b%d", info.Name, t)
		}
	}
	if s := in.Find; s != nil {
		for _, c := range s.Where {
			if c.Op > CondIn || len(c.Path) == 0 {
				return fmt.Errorf("find: invalid condition")
			}
			if err := f.verifyOperand(c.Value); err != nil {
				return fmt.Errorf("find: %w", err)
			}
		}
		for _, k := range s.Sort {
			if len(k.Path) == 0 {
				return fmt.Errorf("find: empty sort key")
			}
		}
		for _, o := range []Operand{s.Page, s.Limit} {
			if o == NoOperand {
				continue
			}
			if err := f.verifyOperand(o); err != nil {
				return fmt.Errorf("find: %w", err)
			}
		}
	}
	return nil
}

func (f *Func) verifyOperand(o Operand) error {
	switch {
	case o == NoOperand:
		return fmt.Errorf("no operand")
	case o.IsSlot() && o.Slot() >= len(f.Slots):
		return fmt.Errorf("no slot r%d", o.Slot())
	case o.IsConst() && o.Const() >= len(f.Consts):
		return fmt.Errorf("no constant %d", o.Const())
	}
	return nil
}

func verifyType(t *Type) error {
	return checkType(t, 0)
}

func checkType(t *Type, depth int) error {
	if t == nil {
		return fmt.Errorf("no type")
	}
	if depth > maxTypeDepth {
		return fmt.Errorf("type too deep")
	}
	if t.Kind >= tKindCount {
		return fmt.Errorf("unknown type kind %d", t.Kind)
	}
	named := t.Kind == TEnum || t.Kind == TValue || t.Kind == TID || t.Kind == TRef || t.Kind == TRecord
	if named != (t.Name != "") {
		return fmt.Errorf("type %s: name", t.Kind)
	}
	switch t.Kind {
	case TOptional, TList, TSet, TPage:
		if t.Key != nil {
			return fmt.Errorf("type %s: extra key", t.Kind)
		}
		return checkType(t.Elem, depth+1)
	case TMap:
		if err := checkType(t.Key, depth+1); err != nil {
			return err
		}
		return checkType(t.Elem, depth+1)
	}
	if t.Elem != nil || t.Key != nil {
		return fmt.Errorf("type %s: extra parameters", t.Kind)
	}
	return nil
}

const maxTypeDepth = 32
