package vm

import (
	"fmt"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// Inner entities of an aggregate (docs/plans/0011, §3.5): entity Order.Stop
// is stored in the aggregate document, in its map field Stop (id → state).
// Its frame is an entity with the aggregate as parent: the state is read
// from the root's map, an event is applied to the inner entity's state,
// written back into the map and goes into the aggregate events
// "Stop.event" with the id as the first argument. It has no separate record
// in the unit of work.

// innerInfo is an inner entity in the schema: the aggregate, the map field
// in it and the aggregate events that record its events.
type innerInfo struct {
	owner  model.TypeRef
	field  int
	events map[model.MethodRef]model.MethodRef // entity event → aggregate event
}

// initInner collects the inner entities of the schema.
func (m *Machine) initInner() {
	for i := range m.schema.Types {
		ti := &m.schema.Types[i]
		for f := range ti.Fields {
			fi := &ti.Fields[f]
			if fi.Inner == 0 {
				continue
			}
			if m.inner == nil {
				m.inner = map[model.TypeRef]*innerInfo{}
			}
			info := &innerInfo{owner: ti.Ref, field: f, events: map[model.MethodRef]model.MethodRef{}}
			for _, mi := range ti.Methods {
				if mi.Inner == fi.Inner {
					info.events[mi.InnerEvent] = mi.Ref
				}
			}
			m.inner[fi.Inner] = info
		}
	}
}

// root returns the aggregate of a frame: its parent for an inner entity.
func (fr *frame) root() *entity {
	if fr.ent != nil && fr.ent.parent != nil {
		return fr.ent.parent
	}
	return fr.ent
}

// innerMap returns the map of records of inner entity t of aggregate e.
func (r *run) innerMap(e *entity, info *innerInfo) model.Value {
	if e == nil || e.state.Kind != model.KindRecord {
		return model.Value{Kind: model.KindMap}
	}
	vals := r.a.Elems(e.state)
	if info.field >= len(vals) || vals[info.field].Kind != model.KindMap {
		return model.Value{Kind: model.KindMap}
	}
	return vals[info.field]
}

// innerEntity returns the frame of inner entity t with identifier id in the
// aggregate of frame fr.
func (r *run) innerEntity(fr *frame, info *innerInfo, t model.TypeRef, id model.Value) (*entity, error) {
	root := fr.root()
	if root == nil || root.t != info.owner {
		return nil, fmt.Errorf("%w: inner entity %d outside its aggregate", ErrUnsupported, t)
	}
	for _, e := range r.ents {
		if e.parent == root && e.t == t && r.a.Equal(e.id, id) {
			return e, nil
		}
	}
	state := model.None
	mp := r.innerMap(root, info)
	if i := indexIn(r.a, mp, id, 2); i >= 0 {
		state = r.a.Elems(mp)[i+1]
	}
	loaded := state.Kind != model.KindNone
	if !loaded {
		def, err := r.m.codec.Default(r.a, t)
		if err != nil {
			return nil, err
		}
		state = def.Value()
	}
	e := &entity{t: t, id: id, loaded: loaded, state: state, parent: root}
	r.ents = append(r.ents, e)
	return e, nil
}

// innerRecord returns the record of inner entity t with identifier id of
// the aggregate of frame fr ([stop]): the fields and the id; none if absent.
func (r *run) innerRecord(fr *frame, info *innerInfo, t model.TypeRef, id model.Value) model.Value {
	if id.Kind == model.KindNone {
		return model.None
	}
	mp := r.innerMap(fr.root(), info)
	i := indexIn(r.a, mp, id, 2)
	if i < 0 {
		return model.None
	}
	return r.withID(t, r.a.Elems(mp)[i+1], id)
}

// innerAll returns the records of inner entity t of the aggregate of frame fr ([Stop]).
func (r *run) innerAll(fr *frame, info *innerInfo, t model.TypeRef) model.Value {
	a := r.a
	mp := r.innerMap(fr.root(), info)
	s := a.Elems(mp)
	m := a.Mark()
	for i := 0; i+1 < len(s); i += 2 {
		a.Push(r.withID(t, s[i+1], s[i]))
	}
	return a.End(model.KindList, 0, m)
}

// withID is a record: the state fields and the identifier last.
func (r *run) withID(t model.TypeRef, state, id model.Value) model.Value {
	a := r.a
	m := a.Mark()
	for _, f := range a.Elems(state) {
		a.Push(f)
	}
	a.Push(id)
	return a.End(model.KindRecord, t, m)
}

// innerEvent records an event of inner entity e: the new state goes into
// the aggregate's map, the event into the aggregate's events.
func (r *run) innerEvent(e *entity, s model.MethodRef, args model.Record) {
	info := r.m.inner[e.t]
	root := e.parent
	a := r.a
	// The map comes before allocating the record: an allocation grows the
	// arena, and a record slice taken earlier would point to old memory.
	mp := r.put(r.innerMap(root, info), e.id, e.state)
	old := a.Elems(root.state)
	ref, state := a.Alloc(len(old))
	copy(state, old)
	state[info.field] = mp
	root.state = model.Value{Kind: model.KindRecord, Type: root.t, Ref: ref}
	m := a.Mark()
	a.Push(e.id)
	for _, v := range a.Fields(args) {
		a.Push(v)
	}
	root.events = append(root.events, model.Event{Name: info.events[s], Args: a.End(model.KindRecord, 0, m).Record(), Seq: r.seq})
}

// innerNewID issues the number of an inner entity with a sequence id: the
// next after the largest in the frame's aggregate and those issued in this
// unit of work.
func (r *run) innerNewID(fr *frame, info *innerInfo, t model.TypeRef) model.Value {
	root := fr.root()
	var max int64
	s := r.a.Elems(r.innerMap(root, info))
	for i := 0; i < len(s); i += 2 {
		if n := s[i].Int(); n > max {
			max = n
		}
	}
	for _, e := range r.ents {
		if e.parent == root && e.t == t && e.id.Int() > max {
			max = e.id.Int()
		}
	}
	id := model.Value{Kind: model.KindID, Type: t, Lo: uint64(max + 1)}
	// The number is taken even if there were no events yet: the frame is created at once.
	if _, err := r.innerEntity(fr, info, t, id); err != nil {
		panic(err)
	}
	return id
}
