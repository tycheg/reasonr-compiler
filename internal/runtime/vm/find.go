package vm

import (
	"fmt"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// An entity or projection record in a register is the fields per the
// schema, then the identifier, which rec.id reads. The engine returns Find
// rows the same way.

// load reads record [id] in a query: the record with the identifier or none.
func (r *run) load(t model.TypeRef, id model.Value) (model.Value, error) {
	if id.Kind == model.KindNone {
		return model.None, nil
	}
	state, _, exists, err := r.env.Load(r.ctx, r.a, t, id)
	if err != nil {
		return model.None, fmt.Errorf("Env.Load: %w", err)
	}
	if !exists {
		return model.None, nil
	}
	a := r.a
	m := a.Mark()
	for _, f := range a.Fields(state) {
		a.Push(f)
	}
	a.Push(id)
	return a.End(model.KindRecord, t, m), nil
}

// find runs a search by a template: condition values, the page and the
// limit come from registers. The result is a list of records or a page.
func (r *run) find(fd *bytecode.Find, regs []model.Value) (model.Value, error) {
	conds := r.conds[:0]
	for _, c := range fd.Where {
		conds = append(conds, model.Cond{Field: c.Field, Op: c.Op, Value: regs[c.Value]})
	}
	r.conds = conds
	s := &r.spec
	*s = model.FindSpec{Type: fd.Type, Index: model.NoIndex, Where: conds, Sort: fd.Sort, Cursor: model.None}
	if fd.Index >= 0 {
		s.Index = model.IndexRef(fd.Index)
	}
	if fd.Page != bytecode.NoReg {
		pr := r.a.Elems(regs[fd.Page])
		s.Page, s.Size, s.Cursor = true, int(pr[0].Int()), pr[1]
		if s.Size < 1 || s.Size > maxPage {
			return model.None, fmt.Errorf("%w: page size %d", ErrLimit, s.Size)
		}
	}
	if fd.Limit != bytecode.NoReg {
		s.Limit = int(regs[fd.Limit].Int())
		if s.Limit < 0 || s.Limit > maxPage {
			return model.None, fmt.Errorf("%w: limit %d", ErrLimit, s.Limit)
		}
	}
	rows, err := r.env.Find(r.ctx, r.a, s)
	if err != nil {
		return model.None, fmt.Errorf("Env.Find: %w", err)
	}
	if rows.Records.Kind != model.KindList {
		rows.Records = r.a.End(model.KindList, 0, r.a.Mark())
	}
	if !s.Page {
		return rows.Records, nil
	}
	m := r.a.Mark()
	r.a.Push(rows.Records)
	r.a.Push(rows.Next)
	return r.a.End(model.KindPage, 0, m), nil
}
