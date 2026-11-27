package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// Fold returns the state of entity t with identifier id obtained by
// applying events events (their apply functions) to the default record:
// this is how the state is restored from the event store. Event arguments
// and id are read from arena src, the result is written into arena a.
func (m *Machine) Fold(ctx context.Context, a, src *model.Arena, t model.TypeRef, id model.Value, events []model.Event) (rec model.Record, err error) {
	defer func() {
		if p := recover(); p != nil {
			rec, err = model.Record{}, caught(p)
		}
	}()
	return m.fold(ctx, a, src, t, id, events)
}

func (m *Machine) fold(ctx context.Context, a, src *model.Arena, t model.TypeRef, id model.Value, events []model.Event) (model.Record, error) {
	r := m.start(ctx, nil, model.Caller{Kind: model.CallerMigration}, nil, time.Time{})
	work := r.a
	r.a = a
	r.a.SetStatic(m.static)
	defer func() {
		model.PutArena(work)
		r.finish()
	}()
	def, err := m.codec.Default(a, t)
	if err != nil {
		return model.Record{}, err
	}
	e := &entity{t: t, id: a.Copy(src, id), loaded: true, state: def.Value()}
	ti := m.schema.Type(t)
	for _, ev := range events {
		if ti != nil && int(ev.Name) < len(ti.Methods) && ti.Methods[ev.Name].Inner != 0 {
			// An event of an inner entity: its state is in the aggregate's map.
			if err := r.foldInner(e, &ti.Methods[ev.Name], a.Copy(src, ev.Args.Value())); err != nil {
				return model.Record{}, fmt.Errorf("event %d: %w", ev.Name, err)
			}
			continue
		}
		fi, ok := m.applies[methodKey{t, ev.Name}]
		if !ok {
			continue // an event without a body does not change the state
		}
		old := a.Elems(e.state)
		ref, state := a.Alloc(len(old))
		copy(state, old)
		next := model.Value{Kind: model.KindRecord, Type: t, Ref: ref}
		args := a.Copy(src, ev.Args.Value())
		if _, err := r.call(fi, a.Elems(args), frame{self: next, ent: e, existed: true}); err != nil {
			return model.Record{}, fmt.Errorf("event %d: %w", ev.Name, err)
		}
		e.state = next
	}
	return e.state.Record(), nil
}

// applyFuncs are the apply functions by event.
func applyFuncs(p *bytecode.Program) map[methodKey]int32 {
	out := map[methodKey]int32{}
	for i := range p.Funcs {
		if f := &p.Funcs[i]; f.Kind == bytecode.FuncApply {
			out[methodKey{f.Owner, f.Method}] = int32(i)
		}
	}
	return out
}

// foldInner applies an event of an inner entity, recorded by an event of
// aggregate e (mi: "Stop.arrived", the first argument is the inner entity
// id), to its record in the aggregate's map.
func (r *run) foldInner(e *entity, mi *model.MethodInfo, args model.Value) error {
	a := r.a
	vals := a.Elems(args)
	if len(vals) == 0 {
		return fmt.Errorf("no inner entity id")
	}
	info := r.m.inner[mi.Inner]
	if info == nil {
		return fmt.Errorf("%w: inner entity %d", ErrUnsupported, mi.Inner)
	}
	in, err := r.innerEntity(&frame{ent: e}, info, mi.Inner, vals[0])
	if err != nil {
		return err
	}
	fi, ok := r.m.applies[methodKey{mi.Inner, mi.InnerEvent}]
	if ok {
		old := a.Elems(in.state)
		ref, state := a.Alloc(len(old))
		copy(state, old)
		next := model.Value{Kind: model.KindRecord, Type: mi.Inner, Ref: ref}
		if _, err := r.call(fi, vals[1:], frame{self: next, ent: in, existed: true}); err != nil {
			return err
		}
		in.state = next
	}
	// The record goes into the aggregate's map, as when running (without an aggregate event).
	mp := r.put(r.innerMap(e, info), in.id, in.state) // before Alloc: see innerEvent
	old := a.Elems(e.state)
	ref, state := a.Alloc(len(old))
	copy(state, old)
	state[info.field] = mp
	e.state = model.Value{Kind: model.KindRecord, Type: e.t, Ref: ref}
	return nil
}
