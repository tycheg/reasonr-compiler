package vm

import (
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// Collections are immutable: every operation builds a new one in the arena
// (decision 8 of the plan, copying). set and map keep insertion order (RFC §7.2).

// indexIn looks for x among the elements xs with step step (2 for map keys)
// and returns the index in the element slice, or -1.
func indexIn(a *model.Arena, xs, x model.Value, step int) int {
	s := a.Elems(xs)
	for i := 0; i < len(s); i += step {
		if a.Equal(s[i], x) {
			return i
		}
	}
	return -1
}

// collection builds a list, a set (repeats are dropped) or a map (a repeated
// key replaces the value in place) from registers.
func (r *run) collection(op bytecode.Op, args []uint32, regs []model.Value) model.Value {
	a := r.a
	m := a.Mark()
	kind := model.KindList
	switch op {
	case bytecode.OpCollList:
		for _, arg := range args {
			a.Push(regs[arg])
		}
	case bytecode.OpCollSet:
		kind = model.KindSet
		for _, arg := range args {
			r.pushUnique(m, regs[arg], model.None, 1)
		}
	default:
		kind = model.KindMap
		for i := 0; i+1 < len(args); i += 2 {
			r.pushUnique(m, regs[args[i]], regs[args[i+1]], 2)
		}
	}
	return a.End(kind, 0, m)
}

// pushUnique pushes onto the arena stack a set element (step 1) or a map
// pair (step 2) if there is no such element or key among those pushed since
// mark; for a map a repeated key replaces the value.
func (r *run) pushUnique(mark int, k, v model.Value, step int) {
	a := r.a
	for i := mark; i < a.Mark(); i += step {
		if a.Equal(a.Stack(i), k) {
			if step == 2 {
				a.SetStack(i+1, v)
			}
			return
		}
	}
	a.Push(k)
	if step == 2 {
		a.Push(v)
	}
}

// appendList returns list xs with element x at the end.
func (r *run) appendList(xs, x model.Value) model.Value {
	a := r.a
	m := a.Mark()
	for _, e := range a.Elems(xs) {
		a.Push(e)
	}
	a.Push(x)
	return a.End(model.KindList, 0, m)
}

// with returns a collection with element x: at the end for a list, at the
// end for a set if it is not there yet.
func (r *run) with(xs, x model.Value) model.Value {
	if xs.Kind == model.KindSet && indexIn(r.a, xs, x, 1) >= 0 {
		return xs
	}
	v := r.appendList(xs, x)
	v.Kind = xs.Kind
	return v
}

// without returns a collection without the elements equal to x.
func (r *run) without(xs, x model.Value) model.Value {
	a := r.a
	if indexIn(a, xs, x, 1) < 0 {
		return xs
	}
	m := a.Mark()
	for _, e := range a.Elems(xs) {
		if !a.Equal(e, x) {
			a.Push(e)
		}
	}
	return a.End(xs.Kind, 0, m)
}

// put returns a map in which key k maps to v: an existing key keeps its
// place, a new one goes to the end.
func (r *run) put(mp, k, v model.Value) model.Value {
	a := r.a
	m := a.Mark()
	for _, e := range a.Elems(mp) {
		a.Push(e)
	}
	r.pushUnique(m, k, v, 2)
	return a.End(model.KindMap, 0, m)
}

// half returns the keys (from 0) or the values (from 1) of a map as a list.
func (r *run) half(mp model.Value, from int) model.Value {
	a := r.a
	m := a.Mark()
	s := a.Elems(mp)
	for i := from; i < len(s); i += 2 {
		a.Push(s[i])
	}
	return a.End(model.KindList, 0, m)
}
