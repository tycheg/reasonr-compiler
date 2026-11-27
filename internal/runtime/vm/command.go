package vm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// entity is an entity in a unit of work: it is loaded once (RFC §24.3). The
// state is copied on every event, so a command's snapshot is simply a
// reference to the state at its start.
type entity struct {
	t       model.TypeRef
	id      model.Value
	loaded  bool   // it existed when loaded
	version uint64 // the version when loaded
	state   model.Value
	events  []model.Event
	parent  *entity // the aggregate of an inner entity (inner.go); nil for a root
}

// exists reports whether the entity exists now: it existed when loaded or
// an event happened to it (RFC §10.3).
func (e *entity) exists() bool { return e.loaded || len(e.events) > 0 }

// frame is the context of a function: the method receiver, the command
// snapshot or the mutable state in apply; the entity of the command, reaction or apply.
type frame struct {
	self    model.Value
	ent     *entity
	existed bool // whether the entity existed at the start of the command ([id])
}

// emitted is an event a function emitted, for on reactions.
type emitted struct {
	ent   *entity
	event model.MethodRef
	args  model.Record
	snap  model.Value // the snapshot of the emitting command
	seq   uint32      // the event number in the unit of work
}

// entity returns an entity of the unit of work, loading it on first access.
func (r *run) entity(t model.TypeRef, id model.Value) (*entity, error) {
	for _, e := range r.ents {
		if e.t == t && r.a.Equal(e.id, id) {
			return e, nil
		}
	}
	state, version, exists, err := r.env.Load(r.ctx, r.a, t, id)
	if err != nil {
		return nil, fmt.Errorf("Env.Load: %w", err)
	}
	var e *entity
	if n := len(r.spare); n > 0 {
		e, r.spare = r.spare[n-1], r.spare[:n-1]
	} else {
		e = &entity{}
	}
	*e = entity{t: t, id: id, loaded: exists, version: version, state: state.Value(), events: e.events[:0]}
	r.ents = append(r.ents, e)
	return e, nil
}

// command calls command fi of entity e: the snapshot is the current state.
func (r *run) command(fi int32, e *entity, args []model.Value) error {
	_, err := r.call(fi, args, frame{self: e.state, ent: e, existed: e.exists()})
	return err
}

// Execute runs a command called from outside: it loads the entity, checks
// access (by the state before the command) and the arguments, runs the
// command and assembles the unit of work or the errors.
func (m *Machine) Execute(ctx context.Context, call model.CommandCall, env model.Env) (out *model.Outcome, err error) {
	defer func() {
		if p := recover(); p != nil {
			out, err = nil, caught(p)
		}
	}()
	return m.execute(ctx, call, env)
}

func (m *Machine) execute(ctx context.Context, call model.CommandCall, env model.Env) (*model.Outcome, error) {
	if call.Event {
		return m.deliver(ctx, call, env)
	}
	fi, ok := m.commands[methodKey{call.Entity, call.Command}]
	if !ok {
		return nil, &Fault{Err: fmt.Errorf("%w: command %d.%d", ErrNotFound, call.Entity, call.Command)}
	}
	r := m.start(ctx, env, call.Caller, call.Arena, call.Now)
	defer r.finish()
	r.root = call
	id := r.importValue(call.Arena, call.ID)
	fail := func(err error) (*model.Outcome, error) {
		model.PutArena(r.a)
		return nil, err
	}
	if id.Kind != model.KindID || id.Type != call.Entity && !r.derived(id.Type, call.Entity) {
		return fail(&Fault{Err: fmt.Errorf("%w: entity identifier %d", ErrNotFound, call.Entity)})
	}
	e, err := r.entity(call.Entity, id)
	if err != nil {
		return fail(&Fault{Err: err})
	}
	r.self = id
	mi := &m.schema.Type(call.Entity).Methods[call.Command]
	external := call.Caller.Kind == model.CallerUser || call.Caller.Kind == model.CallerTest
	switch {
	case external && !mi.Public:
		r.builtinError("forbidden")
	case !r.allowed(mi, e.state):
		r.builtinError("forbidden")
	case mi.Create && e.exists():
		// A creation command: the aggregate with the issued identifier
		// already exists (docs/plans/0011, §3.8).
		r.builtinError("alreadyExists")
	default:
		args, ok, err := r.arguments(methodKey{call.Entity, call.Command}, mi.Params, call.Args, call.Arena)
		if err == nil && ok {
			err = r.command(fi, e, args)
		}
		if err == nil {
			err = r.drain()
		}
		if err != nil {
			return fail(err)
		}
	}
	return r.outcome(), nil
}

// derived reports that the identifier of type t is derived from type base.
func (r *run) derived(t, base model.TypeRef) bool {
	ti := r.m.schema.Type(t)
	return ti != nil && ti.ID == model.IDDerived && ti.IDType == base
}

// outcome assembles a result from the pool: errors or a unit of work.
func (r *run) outcome() *model.Outcome {
	out := model.NewOutcome(r.a)
	if len(r.errors) > 0 {
		out.SetErrors(r.errors)
	} else {
		r.work(out)
	}
	return out
}

// work assembles the unit of work: entities with events in load order and
// effects in call order. The slices are copied into the result's storage:
// run reuses its own.
func (r *run) work(out *model.Outcome) {
	w := out.NewWork()
	for _, op := range r.projections {
		op.Set = out.FieldValues(op.Set)
		w.Projections = append(w.Projections, op)
	}
	w.Timers = append(w.Timers, r.timers...)
	w.Notifications = append(w.Notifications, r.notifications...)
	w.Requests = append(w.Requests, r.requests...)
	w.Forget = append(w.Forget, r.forget...)
	w.Calls = append(w.Calls, r.calls...)
	for _, e := range r.ents {
		if len(e.events) == 0 || e.parent != nil {
			continue // an inner entity is in the state and events of its aggregate
		}
		w.Entities = append(w.Entities, model.EntityChange{
			Type: e.t, ID: e.id, Existed: e.loaded, Version: e.version, Events: out.Events(e.events), State: e.state.Record(),
		})
	}
}

// limit checks a limit on the number of effects.
func limit(n, max int, what string) error {
	if n >= max {
		return fmt.Errorf("%w: more than %d %s", ErrLimit, max, what)
	}
	return nil
}

// event emits an event of the frame's entity: the new state is a copy of
// the current one with the apply function applied.
func (r *run) event(fr *frame, s *bytecode.Site, regs []model.Value) error {
	e := fr.ent
	if e == nil {
		return ErrUnsupported
	}
	if err := limit(r.nevents, r.m.limits.Events, "events"); err != nil {
		return err
	}
	r.nevents++
	args := r.argsRecord(s.Args, regs)
	if s.Func != bytecode.NoFunc {
		old := r.a.Elems(e.state)
		ref, state := r.a.Alloc(len(old))
		copy(state, old)
		next := model.Value{Kind: model.KindRecord, Type: e.t, Ref: ref}
		if _, err := r.call(s.Func, r.a.Fields(args), frame{self: next, ent: e, existed: true}); err != nil {
			return err
		}
		e.state = next
	}
	e.events = append(e.events, model.Event{Name: s.Method, Args: args, Seq: r.seq})
	if e.parent != nil {
		r.innerEvent(e, s.Method, args)
	}
	r.emitted = append(r.emitted, emitted{ent: e, event: s.Method, args: args, snap: fr.self, seq: r.seq})
	r.seq++
	return nil
}

// react calls the on reactions to the events the function emitted (with
// from in the emitted list), in the order the events were emitted.
func (r *run) react(from int) error {
	for i := from; i < len(r.emitted); i++ {
		ev := r.emitted[i]
		for _, fi := range r.m.reactions[methodKey{ev.ent.t, ev.event}] {
			if r.m.prog.Funcs[fi].Kind == bytecode.FuncACL {
				// An ACL reaction of another bounded context runs after the
				// root command, in event order (drain); the bounded context of
				// another process runs it itself from the event stream (deliver).
				if r.local(r.m.aclContext[fi]) {
					r.deferred = append(r.deferred, deferred{fi: fi, ev: ev})
				}
				continue
			}
			if _, err := r.call(fi, r.a.Fields(ev.args), frame{self: ev.snap, ent: ev.ent, existed: true}); err != nil {
				return err
			}
		}
	}
	r.emitted = r.emitted[:from]
	return nil
}

// deferred is an ACL reaction of another bounded context to an event.
type deferred struct {
	fi int32
	ev emitted
}

// drain runs the deferred ACL reactions in event order (docs/plans/0011,
// §3.3): in the same unit of work, with a snapshot of the source entity at
// the time of the event; the reactions they trigger go to the end of the queue.
func (r *run) drain() error {
	for i := 0; i < len(r.deferred); i++ {
		// The queue is in event order: the function queues the reaction of a
		// nested command before the command that called it.
		for j := len(r.deferred) - 1; j > i; j-- { // by insertion, without allocations
			if r.deferred[j].ev.seq < r.deferred[j-1].ev.seq {
				r.deferred[j], r.deferred[j-1] = r.deferred[j-1], r.deferred[j]
			}
		}
		d := r.deferred[i]
		if _, err := r.call(d.fi, r.a.Fields(d.ev.args), frame{self: d.ev.snap, ent: d.ev.ent, existed: true}); err != nil {
			return err
		}
	}
	r.deferred = r.deferred[:0]
	return nil
}

// cmd calls an entity command from a site: argument 0 is the identifier.
// optional is cmd?: nothing happens if the identifier is none or the entity does not exist.
func (r *run) cmd(fr *frame, s *bytecode.Site, regs []model.Value, optional bool) error {
	id := regs[s.Args[0]]
	if id.Kind == model.KindNone {
		if optional {
			return nil
		}
		return fmt.Errorf("%w: a command for none", ErrUnsupported)
	}
	var e *entity
	var err error
	if info := r.m.inner[s.Type]; info != nil {
		e, err = r.innerEntity(fr, info, s.Type, id)
	} else {
		e, err = r.entity(s.Type, id)
	}
	if err != nil {
		return err
	}
	if optional && !e.exists() {
		return nil
	}
	args := r.args[:0]
	for _, a := range s.Args[1:] {
		args = append(args, regs[a])
	}
	r.args = args
	return r.command(s.Func, e, args)
}

// notify adds a notification: argument 0 is the recipient; NoReg sends it
// to the initiator, none does not send it (RFC §13).
func (r *run) notify(s *bytecode.Site, regs []model.Value, fr *frame) error {
	to := model.None
	if s.Args[0] != bytecode.NoReg {
		if to = regs[s.Args[0]]; to.Kind == model.KindNone {
			return nil
		}
	}
	if err := limit(len(r.notifications)+len(r.requests), r.m.limits.Messages, "notifications and external requests"); err != nil {
		return err
	}
	r.notifications = append(r.notifications, model.Notification{
		Owner: s.Type, Name: s.Method, Args: r.argsRecord(s.Args[1:], regs), To: to, Subjects: r.argSubjects(s, fr),
	})
	return nil
}

// upsert adds a projection upsert: the first field is id, the rest are the passed ones.
func (r *run) upsert(s *bytecode.Site, regs []model.Value) {
	op := model.ProjectionOp{Kind: model.ProjectionUpsert, Projection: s.Type, Seq: r.seq}
	r.seq++
	start := len(r.sets)
	for i, f := range s.Fields {
		v := regs[s.Args[i]]
		if f == bytecode.IDField {
			op.ID = v
			continue
		}
		r.sets = append(r.sets, model.FieldValue{Field: f, Value: v})
	}
	op.Set = r.sets[start:len(r.sets):len(r.sets)]
	r.projections = append(r.projections, op)
}

// schedule sets a timer for a command of the frame's entity: argument 0 is the instant.
func (r *run) schedule(fr *frame, s *bytecode.Site, regs []model.Value) (model.Value, error) {
	if fr.ent == nil {
		return model.None, ErrUnsupported
	}
	if err := limit(len(r.timers), r.m.limits.Timers, "timers"); err != nil {
		return model.None, err
	}
	id, err := r.env.NewID(r.ctx, r.a, 0)
	if err != nil {
		return model.None, fmt.Errorf("Env.NewID: %w", err)
	}
	r.timers = append(r.timers, model.TimerOp{
		Kind: model.TimerSchedule, ID: id, At: regs[s.Args[0]].Timestamp(),
		Entity: fr.ent.t, EntityID: fr.ent.id, Command: s.Method, Args: r.argsRecord(s.Args[1:], regs),
		Subjects: r.paramSubjects(fr, r.m.schema.Type(fr.ent.t).Methods[s.Method].Params),
	})
	return id, nil
}

// request adds an external request. The idempotency key is deterministic:
// it depends on the root call, now and the request number, so a retried
// run gives the same key.
func (r *run) request(s *bytecode.Site, regs []model.Value, fr *frame) error {
	if err := limit(len(r.notifications)+len(r.requests), r.m.limits.Messages, "notifications and external requests"); err != nil {
		return err
	}
	r.requests = append(r.requests, model.ExternalRequest{
		System: s.Type, Request: s.Method, Args: r.argsRecord(s.Args, regs), Key: r.requestKey(),
		Subjects: r.argSubjects(s, fr),
	})
	return nil
}

// callOut adds a call of a public model command by a fake: argument 0 is the
// entity identifier. The engine runs it after commit from the role of the
// fake's system (RFC §18).
func (r *run) callOut(s *bytecode.Site, regs []model.Value, fr *frame) error {
	if fr.ent == nil {
		return ErrUnsupported
	}
	if err := limit(len(r.notifications)+len(r.requests)+len(r.calls), r.m.limits.Messages, "notifications, external requests and calls"); err != nil {
		return err
	}
	r.calls = append(r.calls, model.FakeCall{
		System: fr.ent.t, Entity: s.Type, ID: regs[s.Args[0]], Command: s.Method, Args: r.argsRecord(s.Args[1:], regs),
	})
	return nil
}

func (r *run) requestKey() model.Value {
	b := binary.BigEndian.AppendUint32(r.kbuf[:0], uint32(r.root.Entity))
	b = binary.BigEndian.AppendUint32(b, uint32(r.root.Command))
	b = binary.BigEndian.AppendUint64(b, r.now.Lo)
	b = binary.BigEndian.AppendUint64(b, r.now.Hi)
	b = binary.BigEndian.AppendUint32(b, uint32(len(r.requests)))
	b = r.a.AppendFormat(b, r.root.ID)
	r.kbuf = b
	sum := sha256.Sum256(b)
	var u [16]byte
	copy(u[:], sum[:])
	u[6] = u[6]&0x0f | 0x80 // version 8: a custom scheme
	u[8] = u[8]&0x3f | 0x80
	return model.UUID(u)
}

// argSubjects computes the personal data subjects of arguments by the paths
// from self (RFC §20). A subject with id none is
// skipped: the argument holds no data of it.
func (r *run) argSubjects(s *bytecode.Site, fr *frame) []model.ArgSubject {
	if len(s.Subjects) == 0 || fr.ent == nil {
		return nil
	}
	var out []model.ArgSubject
	for _, ss := range s.Subjects {
		if id := r.subjectAt(fr, ss.Path); id.Kind == model.KindID {
			out = append(out, model.ArgSubject{Arg: int(ss.Arg), Subject: model.Subject{Type: ss.Type, ID: id}})
		}
	}
	return out
}

// paramSubjects returns the subjects of the personal parameters of a timer
// command: their paths start at the frame's entity, on which the timer will
// call the command (RFC §20).
func (r *run) paramSubjects(fr *frame, params []model.FieldInfo) []model.ArgSubject {
	var out []model.ArgSubject
	for i := range params {
		p := params[i].Personal
		if p == nil {
			continue
		}
		id := r.subjectAt(fr, p.Subject)
		if id.Kind != model.KindID {
			continue
		}
		t := p.SubjectType
		if t == 0 {
			t = id.Type
		}
		out = append(out, model.ArgSubject{Arg: i, Subject: model.Subject{Type: t, ID: id}})
	}
	return out
}

// subjectAt returns the identifier of a subject by the path from the
// frame's self; an empty path is the entity itself.
func (r *run) subjectAt(fr *frame, path model.FieldPath) model.Value {
	if len(path) == 0 {
		return fr.ent.id
	}
	v := fr.self
	for _, f := range path {
		if f == bytecode.IDField {
			v = fr.ent.id
			continue
		}
		if v.Kind != model.KindRecord || uint32(f) >= v.Ref.Len {
			return model.None
		}
		v = r.a.Elems(v)[f]
	}
	return v
}

// forget adds a subject for crypto-shredding (without repeats).
func (r *run) forgetSubject(t model.TypeRef, id model.Value) {
	for _, s := range r.forget {
		if s.Type == t && r.a.Equal(s.ID, id) {
			return
		}
	}
	r.forget = append(r.forget, model.Subject{Type: t, ID: id})
}

// exists is [x] in a command: whether the entity exists. Its own entity as
// of the start of the command (RFC §10.3).
func (r *run) exists(fr *frame, t model.TypeRef, id model.Value) (bool, error) {
	if id.Kind == model.KindNone {
		return false, nil
	}
	if fr.ent != nil && fr.ent.t == t && r.a.Equal(fr.ent.id, id) {
		return fr.existed, nil
	}
	e, err := r.entity(t, id)
	if err != nil {
		return false, err
	}
	return e.exists(), nil
}

// local reports whether this engine process runs bounded context ctx
// (CommandCall.Contexts, docs/plans/0011, B6).
func (r *run) local(ctx string) bool {
	if r.root.Contexts == nil {
		return true
	}
	return slices.Contains(r.root.Contexts, ctx)
}

// deliver runs the ACL reactions of this process's bounded contexts to an
// event another bounded context's process committed: event Command of
// entity Entity with identifier ID and arguments Args. The unit of work is
// the effects of the reactions; the reactions they trigger follow, as in drain.
func (m *Machine) deliver(ctx context.Context, call model.CommandCall, env model.Env) (*model.Outcome, error) {
	r := m.start(ctx, env, model.Caller{Kind: model.CallerMigration, Subject: model.None}, call.Arena, call.Now)
	defer r.finish()
	r.root = call
	fail := func(err error) (*model.Outcome, error) {
		model.PutArena(r.a)
		return nil, err
	}
	id := r.importValue(call.Arena, call.ID)
	if id.Kind != model.KindID || id.Type != call.Entity {
		return fail(&Fault{Err: fmt.Errorf("%w: entity identifier %d", ErrNotFound, call.Entity)})
	}
	src := &entity{t: call.Entity, id: id, loaded: true, state: model.None}
	args := r.a.Copy(call.Arena, call.Args.Value()).Record()
	ev := emitted{ent: src, event: call.Command, args: args, snap: model.None, seq: 0}
	for _, fi := range m.reactions[methodKey{call.Entity, call.Command}] {
		if m.prog.Funcs[fi].Kind == bytecode.FuncACL && r.local(m.aclContext[fi]) {
			r.deferred = append(r.deferred, deferred{fi: fi, ev: ev})
		}
	}
	if err := r.drain(); err != nil {
		return fail(err)
	}
	return r.outcome(), nil
}
